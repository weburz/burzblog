import base64
import hashlib
from dataclasses import replace

import pytest
from fastapi.testclient import TestClient

from burzblog import server
from burzblog.api import create_app
from burzblog.auth import Authentication
from burzblog.errors import ContentError
from burzblog.settings import Settings

ORIGIN = "http://localhost:3000"
CONTAINER_BIND = "0.0.0.0"  # noqa: S104 - asserting the explicit container opt-in
BOOT = b'window.__NUXT__={config:{app:{baseURL:"/"}}}'
IMPORTS = b'{"imports":{"#entry":"/_nuxt/entry.js"}}'
SHELL = (
    b"<!DOCTYPE html><html><head>"
    b'<script type="importmap">' + IMPORTS + b"</script>"
    b'<script type="module" src="/_nuxt/entry.js"></script></head><body>'
    b"<script>" + BOOT + b"</script>"
    b'<script type="application/json" id="__NUXT_DATA__">[1]</script>'
    b"</body></html>"
)


def digest(script: bytes) -> str:
    return "'sha256-" + base64.b64encode(hashlib.sha256(script).digest()).decode() + "'"


@pytest.fixture
def web_settings(tmp_path):
    content = tmp_path / "content"
    (content / "posts").mkdir(parents=True)
    web = tmp_path / "web"
    (web / "_nuxt").mkdir(parents=True)
    (web / "index.html").write_bytes(SHELL)
    (web / "_nuxt/entry.js").write_text("console.log('app')\n")
    (tmp_path / "outside.txt").write_text("private operator file")
    settings = Settings(content, tmp_path / "state", ORIGIN, web_root=web)
    Authentication(settings.state_directory).initialize("fixture only password")
    return settings


def test_shell_is_served_same_origin_with_hash_based_csp(web_settings, tmp_path):
    (web_settings.web_root / "_nuxt/escape.js").symlink_to(tmp_path / "outside.txt")
    with TestClient(create_app(web_settings), base_url=ORIGIN) as client:
        shell = client.get("/")
        assert shell.status_code == 200
        assert shell.content == SHELL
        assert shell.headers["content-type"].startswith("text/html")
        assert shell.headers["cache-control"] == "no-cache"
        policy = dict(
            directive.split(" ", 1)
            for directive in shell.headers["content-security-policy"].split("; ")
        )
        scripts = policy["script-src"].split()
        assert sorted(scripts) == sorted(["'self'", digest(IMPORTS), digest(BOOT)])
        assert "unsafe-inline" not in policy["script-src"]
        assert "unsafe-eval" not in shell.headers["content-security-policy"]
        assert policy["frame-ancestors"] == "'none'"
        assert policy["default-src"] == "'none'"
        for header, value in {
            "x-content-type-options": "nosniff",
            "x-frame-options": "DENY",
            "referrer-policy": "no-referrer",
            "x-robots-tag": "noindex",
        }.items():
            assert shell.headers[header] == value
        assert len(shell.headers.get_list("cache-control")) == 1

        asset = client.get("/_nuxt/entry.js")
        assert asset.status_code == 200
        assert asset.headers["cache-control"] == "public, max-age=31536000, immutable"
        assert client.get("/_nuxt/escape.js").status_code == 404
        assert client.get("/_nuxt/../outside.txt").status_code == 404
        assert client.get("/_nuxt/%2e%2e/outside.txt").status_code == 404
        assert client.get("/index.html").status_code == 404

        # The API keeps its own deny-by-default policy and is still authenticated.
        api = client.get("/api/posts")
        assert api.status_code == 401
        assert api.headers["cache-control"] == "no-store"
        assert api.headers["content-security-policy"].startswith("default-src 'none'")
        assert client.get("/", headers={"host": "attacker.invalid"}).status_code == 400


def test_api_only_mode_serves_no_shell(web_settings):
    with TestClient(
        create_app(replace(web_settings, web_root=None)), base_url=ORIGIN
    ) as client:
        assert client.get("/").status_code == 404
        assert client.get("/_nuxt/entry.js").status_code == 404


@pytest.mark.parametrize("broken", ["missing-index", "missing-assets", "relative"])
def test_incomplete_web_root_is_refused(web_settings, broken):
    web = web_settings.web_root
    if broken == "missing-index":
        (web / "index.html").unlink()
    elif broken == "missing-assets":
        (web / "_nuxt/entry.js").unlink()
        (web / "_nuxt").rmdir()
    else:
        web = web.relative_to(web.parent.parent)
    with pytest.raises(ContentError, match="Web root"):
        create_app(replace(web_settings, web_root=web))


def test_server_binds_loopback_unless_container_host_is_explicit(
    web_settings, monkeypatch
):
    monkeypatch.setenv("BURZBLOG_CONTENT_ROOT", str(web_settings.content_root))
    monkeypatch.setenv("BURZBLOG_STATE_DIR", str(web_settings.state_directory))
    monkeypatch.setenv("BURZBLOG_ORIGIN", ORIGIN)
    monkeypatch.setenv("BURZBLOG_WEB_ROOT", str(web_settings.web_root))
    for name in ("URL", "USERNAME", "TOKEN_FILE", "CA_FILE"):
        monkeypatch.delenv(f"BURZBLOG_REMOTE_{name}", raising=False)
    received = {}
    monkeypatch.setattr(
        "uvicorn.run", lambda app, **kwargs: received.update(kwargs, app=app)
    )
    monkeypatch.setattr("sys.argv", ["burzblog-server", "serve"])
    assert server.main() == 0
    assert received["host"] == "127.0.0.1"
    monkeypatch.setattr(
        "sys.argv",
        ["burzblog-server", "serve", "--host", CONTAINER_BIND, "--port", "8080"],
    )
    assert server.main() == 0
    assert received["host"] == CONTAINER_BIND and received["port"] == 8080
    with TestClient(received["app"], base_url=ORIGIN) as client:
        assert client.get("/").content == SHELL
