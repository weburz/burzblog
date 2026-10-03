"""Static deployment-contract checks; runtime behavior is scripts/docker-smoke.py."""

import re
from pathlib import Path

import pytest
from ruamel.yaml import YAML

PROJECT = Path(__file__).resolve().parents[2]


def service(name: str) -> dict:
    return YAML(typ="safe").load((PROJECT / name).read_text())["services"]["burzblog"]


@pytest.fixture(scope="module")
def dockerfile() -> str:
    return (PROJECT / "Dockerfile").read_text()


def test_local_and_distribution_compose_define_the_same_service():
    local, distribution = service("compose.yaml"), service("compose.dist.yaml")
    assert local.pop("build") == "."
    assert local.pop("image") == "burzblog:local"
    assert distribution.pop("image").startswith("${BURZBLOG_IMAGE:?")
    assert local == distribution


@pytest.mark.parametrize("name", ["compose.yaml", "compose.dist.yaml"])
def test_compose_hardening_and_persistence(name):
    burzblog = service(name)
    assert burzblog["read_only"] is True
    assert burzblog["cap_drop"] == ["ALL"]
    assert burzblog["security_opt"] == ["no-new-privileges:true"]
    assert burzblog["restart"] == "unless-stopped"
    # Longer than the 30 s Git network deadline (git_mutations.py) plus margin.
    assert int(burzblog["stop_grace_period"].removesuffix("s")) > 30
    assert re.fullmatch(
        r"\$\{BURZBLOG_UID:\?[^}]+\}:\$\{BURZBLOG_GID:\?[^}]+\}", burzblog["user"]
    )
    assert not {"privileged", "network_mode", "pid", "ipc", "cap_add", "devices"} & set(
        burzblog
    )
    assert all(port.startswith("127.0.0.1:") for port in burzblog["ports"])
    mounts = {volume["target"]: volume for volume in burzblog["volumes"]}
    assert set(mounts) == {"/site", "/state", "/secrets"}
    for volume in mounts.values():
        # Bind mounts only: no anonymous/named volumes that image replacement could orphan.
        assert volume["type"] == "bind"
        assert volume["bind"]["create_host_path"] is False
    assert mounts["/secrets"]["read_only"] is True
    environment = burzblog["environment"]
    assert environment["BURZBLOG_STATE_DIR"] == "/state"
    assert environment["BURZBLOG_CONTENT_ROOT"].startswith("/site/")
    # Secrets are referenced by file path only; never an inline secret variable.
    assert not [key for key in environment if re.search(r"TOKEN$|PASSWORD|SECRET", key)]


def test_runtime_image_contract(dockerfile):
    stages = re.split(r"^FROM ", dockerfile, flags=re.MULTILINE)[1:]
    for stage in stages:
        assert re.match(r"\S+@sha256:[0-9a-f]{64} AS \w+", stage), stage.splitlines()[0]
    runtime = stages[-1]
    assert runtime.split()[2] == "runtime"
    assert re.search(r"^USER \d+:\d+$", runtime, re.MULTILINE)
    assert "HEALTHCHECK" in runtime and "/api/session" in runtime
    assert 'ENTRYPOINT ["tini", "--", "burzblog-server"]' in runtime
    assert "BURZBLOG_WEB_ROOT=/app/web" in runtime
    # Build tooling and source stay in earlier stages.
    assert not re.search(r"\b(uv|pnpm|npm|node)\b\s", runtime.replace("/app/.venv", ""))
    assert "COPY src" not in runtime and "COPY web" not in runtime
    assert "pip uninstall" in runtime
    for forbidden in ("VOLUME", "ADD ", "--privileged", "chmod 777"):
        assert forbidden not in dockerfile


def test_build_context_excludes_private_and_generated_files():
    ignore = (PROJECT / ".dockerignore").read_text().splitlines()
    assert ignore[0] == "**"
    allowed = [line for line in ignore if line.startswith("!")]
    assert allowed == [
        "!Dockerfile",
        "!pyproject.toml",
        "!uv.lock",
        "!README.md",
        "!src/",
        "!src/**",
        "!web/",
        "!web/**",
    ]
    for excluded in ("web/node_modules/", "web/.output/", "**/.env", "**/.env.*"):
        assert excluded in ignore


def test_environment_example_contains_no_secret_values():
    example = (PROJECT / ".env.example").read_text()
    assert "BURZBLOG_REMOTE_TOKEN=" not in example
    assert "BURZBLOG_REMOTE_PASSWORD" not in example
    assert re.search(r"^# BURZBLOG_REMOTE_TOKEN_FILE=/secrets/", example, re.MULTILINE)
    for line in example.splitlines():
        assert not re.search(r"(ghp_|glpat-|github_pat_|xox[bp]-)", line)


def test_reverse_proxy_examples_keep_the_trust_boundary():
    caddy = (PROJECT / "deploy/Caddyfile").read_text()
    nginx = (PROJECT / "deploy/nginx.conf").read_text()
    # Both forward only to the loopback-published container port.
    assert re.search(r"^\s*reverse_proxy 127\.0\.0\.1:8080$", caddy, re.MULTILINE)
    assert "proxy_pass http://127.0.0.1:8080;" in nginx
    # The application's Origin/Host checks need the original Host header.
    assert "proxy_set_header Host $host;" in nginx
    assert "header_up Host" not in caddy  # Caddy preserves Host by default.
    # Request bodies must fit the application's 3 MiB limit plus JSON overhead.
    assert "max_size 4MB" in caddy and "client_max_body_size 4m;" in nginx
    for config in (caddy, nginx):
        assert "Strict-Transport-Security" in config
        assert "X-Forwarded" not in config.replace("X-Forwarded-*", "")
    assert re.search(r"return 30[18] https://", nginx)
    assert "limit_conn_zone" in nginx and "limit_conn burzblog_per_ip" in nginx
