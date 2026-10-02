import base64
import logging
import os
import secrets
import subprocess
from pathlib import Path

import pytest
from fastapi.testclient import TestClient
from support.git_https import basic, start

from burzblog import cli, server
from burzblog.api import create_app
from burzblog.auth import Authentication
from burzblog.errors import ConflictError, ContentError
from burzblog.git_credentials import (
    CredentialError,
    RemoteCredential,
    validate_certificate_authority,
)
from burzblog.git_mutations import GitChanges, Remote
from burzblog.git_process import GitError, RemoteError, isolated_environment
from burzblog.settings import Settings

USERNAME = "fixture-user"


def git(root, *arguments, check=True):
    return subprocess.run(  # noqa: S603 - controlled local fixtures
        ["git", "-C", str(root), *arguments],  # noqa: S607 - developer Git
        check=check,
        capture_output=True,
        text=True,
    )


def secret_file(directory: Path, value: str, mode: int = 0o600) -> Path:
    directory.mkdir(mode=0o700, exist_ok=True)
    path = directory / "token"
    path.write_text(value)
    path.chmod(mode)
    return path


@pytest.fixture
def token():
    return "fixture-" + secrets.token_urlsafe(24)


@pytest.fixture
def private(tmp_path, token):
    fixture, server_ = start(tmp_path / "remote", basic(USERNAME, token))
    yield fixture
    server_.shutdown()
    server_.server_close()


@pytest.fixture
def workspace(tmp_path):
    root = tmp_path / "repo"
    root.mkdir()
    git(root, "init", "--initial-branch=main")
    git(root, "config", "user.name", "Test")
    git(root, "config", "user.email", "test@example.invalid")
    content = root / "content"
    (content / "posts").mkdir(parents=True)
    (content / ".archive").mkdir()
    (content / "posts/a.md").write_text("initial\n")
    git(root, "add", "content/posts/a.md")
    git(root, "-c", "commit.gpgsign=false", "commit", "-m", "initial")
    return root, content, GitChanges(content)


def remote_for(fixture, file, url=None):
    return Remote(
        "origin",
        url or fixture.url,
        RemoteCredential(USERNAME, file) if file else None,
        fixture.certificate_authority,
    )


def leaked(token: str, *roots: Path) -> list[Path]:
    """Every file containing the secret or its Basic-encoded form."""
    needles = (
        token.encode(),
        base64.b64encode(f"{USERNAME}:{token}".encode()),
    )
    found = []
    for root in roots:
        for directory, _, files in os.walk(root):
            for name in files:
                path = Path(directory) / name
                data = path.read_bytes()
                if any(needle in data for needle in needles):
                    found.append(path)
    return found


def external_clone(tmp_path, bare):
    other = tmp_path / "other"
    git(tmp_path, "clone", str(bare), str(other))
    git(other, "config", "user.name", "External")
    git(other, "config", "user.email", "external@example.invalid")
    return other


def external_commit(other, text):
    (other / "content/posts/a.md").write_text(text)
    git(other, "add", "content/posts/a.md")
    git(other, "-c", "commit.gpgsign=false", "commit", "-m", text.strip())
    git(other, "push", "origin", "main")


def test_authenticated_https_push_fetch_and_update(tmp_path, workspace, private, token):
    root, content, changes = workspace
    remote = remote_for(private, secret_file(tmp_path / "secrets", token + "\n"))
    config_before = (root / ".git/config").read_bytes()
    changes.push(remote, "main")
    assert (
        git(private.repository, "show", "main:content/posts/a.md").stdout == "initial\n"
    )
    other = external_clone(tmp_path, private.repository)
    external_commit(other, "remote edit\n")
    changes.fetch(remote)
    assert (
        git(root, "rev-parse", "refs/remotes/origin/main").stdout
        == git(other, "rev-parse", "HEAD").stdout
    )
    changes.update("origin", "main")
    assert (content / "posts/a.md").read_text() == "remote edit\n"
    assert private.requests and all(
        seen.authorization == basic(USERNAME, token) for seen in private.requests
    )
    assert (root / ".git/config").read_bytes() == config_before
    assert not (root / ".git/FETCH_HEAD").exists()
    assert leaked(token, root, private.repository) == []


def test_invalid_credential_is_refused_without_leaking(
    tmp_path, workspace, private, token, capsys, caplog
):
    root, _, changes = workspace
    wrong = "wrong-" + token
    remote = remote_for(private, secret_file(tmp_path / "secrets", wrong))
    caplog.set_level(logging.DEBUG)
    with pytest.raises(RemoteError) as push_failure:
        changes.push(remote, "main")
    with pytest.raises(RemoteError) as fetch_failure:
        changes.fetch(remote)
    assert git(
        private.repository, "rev-parse", "--verify", "main", check=False
    ).returncode
    captured = capsys.readouterr()
    for text in (
        str(push_failure.value),
        repr(push_failure.value),
        str(fetch_failure.value),
        captured.out,
        captured.err,
        caplog.text,
        repr(remote),
    ):
        assert wrong not in text and token not in text
    assert "credentials" in str(push_failure.value)
    assert leaked(wrong, root) == []


def test_secret_travels_only_in_child_environment(
    tmp_path, workspace, private, token, monkeypatch
):
    _, _, changes = workspace
    remote = remote_for(private, secret_file(tmp_path / "secrets", token))
    launched = []
    original = subprocess.Popen

    def recording(command, *args, **kwargs):
        if any(str(argument).startswith("--git-dir=") for argument in command):
            launched.append((command, kwargs["env"]))
        return original(command, *args, **kwargs)

    monkeypatch.setattr("burzblog.git_process.subprocess.Popen", recording)
    changes.push(remote, "main")
    [(push_command, environment)] = [entry for entry in launched if "push" in entry[0]]
    assert not any(token in argument for argument in push_command)
    assert not any(
        base64.b64encode(f"{USERNAME}:{token}".encode()).decode() in argument
        for argument in push_command
    )
    scoped = [
        environment[key.replace("KEY", "VALUE")]
        for key, value in environment.items()
        if key.startswith("GIT_CONFIG_KEY_") and value.endswith(".extraHeader")
    ]
    assert scoped == ["Authorization: " + basic(USERNAME, token)]
    assert environment["HOME"] == os.devnull


def test_tls_requires_a_trusted_authority_before_sending_credentials(
    tmp_path, workspace, private, token
):
    _, _, changes = workspace
    remote = Remote(
        "origin",
        private.url,
        RemoteCredential(USERNAME, secret_file(tmp_path / "s", token)),
    )
    with pytest.raises(RemoteError):
        changes.fetch(remote)
    assert private.requests == []


def test_redirects_are_not_followed_and_credentials_not_forwarded(
    tmp_path, workspace, token
):
    _, _, changes = workspace
    target, target_server = start(tmp_path / "target", None, name="target.git")
    redirector, redirect_server = start(
        tmp_path / "redirector", None, redirect=target.url.rsplit("/", 1)[0]
    )
    try:
        remote = Remote(
            "origin",
            redirector.url,
            RemoteCredential(USERNAME, secret_file(tmp_path / "secrets", token)),
            redirector.certificate_authority,
        )
        with pytest.raises(RemoteError):
            changes.fetch(remote)
        assert redirector.requests
        assert target.requests == []
    finally:
        for running in (target_server, redirect_server):
            running.shutdown()
            running.server_close()


def test_non_fast_forward_push_is_refused_over_https(
    tmp_path, workspace, private, token
):
    _, content, changes = workspace
    remote = remote_for(private, secret_file(tmp_path / "secrets", token))
    changes.push(remote, "main")
    other = external_clone(tmp_path, private.repository)
    external_commit(other, "remote divergence\n")
    (content / "posts/a.md").write_text("local divergence\n")
    changes.commit(["posts/a.md"], "local")
    with pytest.raises(ConflictError, match="Nothing was overwritten"):
        changes.push(remote, "main")
    assert (
        git(private.repository, "show", "main:content/posts/a.md").stdout
        == "remote divergence\n"
    )
    assert (content / "posts/a.md").read_text() == "local divergence\n"


@pytest.mark.parametrize(
    ("key", "value"),
    [
        ("credential.helper", "!touch {marker}"),
        ("credential.https://127.0.0.1.helper", "!touch {marker}"),
        ("http.extraHeader", "Authorization: Basic ZXZpbDpldmls"),
        ("http.sslVerify", "false"),
        ("url.file:///.insteadOf", "https://"),
        ("remote.origin.uploadpack", "touch {marker}"),
        ("core.askPass", "{marker}"),
        ("core.sshCommand", "touch {marker}"),
        ("include.path", "{marker}"),
    ],
)
def test_hostile_repository_configuration_is_refused_before_contact(
    tmp_path, workspace, private, token, key, value
):
    root, _, changes = workspace
    marker = tmp_path / "executed"
    git(root, "config", "--add", key, value.format(marker=marker))
    remote = remote_for(private, secret_file(tmp_path / "secrets", token))
    with pytest.raises(GitError):
        changes.fetch(remote)
    with pytest.raises(GitError):
        changes.push(remote, "main")
    assert private.requests == []
    assert not marker.exists()


def test_ambient_user_credentials_are_never_discovered(
    tmp_path, workspace, private, token, monkeypatch
):
    _, _, changes = workspace
    home = tmp_path / "home"
    home.mkdir()
    (home / ".netrc").write_text(
        f"machine 127.0.0.1 login {USERNAME} password {token}\n"
    )
    (home / ".gitconfig").write_text(
        f'[credential]\n\thelper = "!f() {{ echo username={USERNAME}; echo password={token}; }}; f"\n'
    )
    (home / ".git-credentials").write_text(f"https://{USERNAME}:{token}@127.0.0.1\n")
    for name, value in {
        "HOME": str(home),
        "XDG_CONFIG_HOME": str(home),
        "GIT_ASKPASS": "/bin/echo",
        "SSH_ASKPASS": "/bin/echo",
        "GIT_SSL_NO_VERIFY": "1",
        "GIT_CONFIG_COUNT": "1",
        "GIT_CONFIG_KEY_0": "http.extraHeader",
        "GIT_CONFIG_VALUE_0": "Authorization: " + basic(USERNAME, token),
    }.items():
        monkeypatch.setenv(name, value)
    environment = isolated_environment()
    assert environment["HOME"] == os.devnull
    assert not {
        "GIT_ASKPASS",
        "SSH_ASKPASS",
        "GIT_SSL_NO_VERIFY",
        "GIT_CONFIG_COUNT",
    } & set(environment)
    with pytest.raises(RemoteError):
        changes.fetch(remote_for(private, None))
    assert private.requests and all(
        seen.authorization is None for seen in private.requests
    )


def test_credential_free_remotes_remain_supported(tmp_path, workspace):
    root, _, changes = workspace
    public, public_server = start(tmp_path / "public", None, name="public.git")
    try:
        changes.push(remote_for(public, None), "main")
        changes.fetch(remote_for(public, None))
        assert all(seen.authorization is None for seen in public.requests)
    finally:
        public_server.shutdown()
        public_server.server_close()
    bare = tmp_path / "local.git"
    git(tmp_path, "init", "--bare", "--initial-branch=main", str(bare))
    changes.push(Remote("local", str(bare)), "main")
    changes.fetch(Remote("local", str(bare)))
    assert git(root, "rev-parse", "refs/remotes/local/main").returncode == 0


@pytest.mark.parametrize(
    ("contents", "mode"),
    [
        ("", 0o600),
        ("\n", 0o600),
        ("two\nlines", 0o600),
        ("nul\0byte", 0o600),
        ("x" * 4097, 0o600),
        ("group-readable", 0o640),
        ("world-readable", 0o644),
    ],
)
def test_malformed_or_exposed_secret_files_are_refused(tmp_path, contents, mode):
    credential = RemoteCredential(USERNAME, secret_file(tmp_path / "s", contents, mode))
    with pytest.raises(CredentialError) as failure:
        credential.validate()
    assert contents.strip() == "" or contents not in str(failure.value)


def test_secret_file_kind_and_path_failures(tmp_path):
    directory = tmp_path / "secrets"
    real = secret_file(directory, "valid-token")
    link = directory / "link"
    link.symlink_to(real)
    fifo = directory / "fifo"
    os.mkfifo(fifo, 0o600)
    (directory / "binary").write_bytes(b"\xff\xfe")
    (directory / "binary").chmod(0o600)
    for path in (link, fifo, directory, directory / "missing", directory / "binary"):
        with pytest.raises(CredentialError):
            RemoteCredential(USERNAME, path).validate()
    with pytest.raises(CredentialError):
        RemoteCredential(USERNAME, Path("relative/token")).validate()
    linked_parent = tmp_path / "linked"
    linked_parent.symlink_to(directory)
    with pytest.raises(CredentialError):
        RemoteCredential(USERNAME, linked_parent / "token").validate()
    for username in ("", "with:colon", "with space", "x" * 257, "tab\tname"):
        with pytest.raises(CredentialError):
            RemoteCredential(username, real).validate()
    RemoteCredential(USERNAME, real).validate()
    readonly = secret_file(tmp_path / "readonly", "valid-token", 0o400)
    RemoteCredential(USERNAME, readonly).validate()


def test_certificate_authority_validation(tmp_path):
    directory = tmp_path / "trust"
    directory.mkdir()
    bundle = directory / "ca.pem"
    bundle.write_text("not a certificate")
    bundle.chmod(0o644)
    with pytest.raises(CredentialError):
        validate_certificate_authority(bundle)
    bundle.write_text("-----BEGIN CERTIFICATE-----\n")
    bundle.chmod(0o664)
    with pytest.raises(CredentialError):
        validate_certificate_authority(bundle)
    bundle.chmod(0o644)
    validate_certificate_authority(bundle)


def test_credentials_only_apply_to_https_remotes(tmp_path):
    token_file = secret_file(tmp_path / "secrets", "valid-token")
    credential = RemoteCredential(USERNAME, token_file)
    for url in (
        "http://example.invalid/r.git",
        "ssh://git@example.invalid/r.git",
        "git@example.invalid:r.git",
    ):
        with pytest.raises(GitError):
            Remote("origin", url, credential).transport()
    with pytest.raises(GitError):
        Remote("origin", str(tmp_path), credential).transport()
    with pytest.raises(GitError):
        Remote("origin", "https://user:pass@example.invalid/r.git").transport()


def configure(monkeypatch, tmp_path, **values):
    content = tmp_path / "content"
    content.mkdir(exist_ok=True)
    monkeypatch.setenv("BURZBLOG_CONTENT_ROOT", str(content))
    monkeypatch.setenv("BURZBLOG_STATE_DIR", str(tmp_path / "state"))
    for name in (
        "URL",
        "NAME",
        "USERNAME",
        "TOKEN_FILE",
        "CA_FILE",
        "TOKEN",
        "PASSWORD",
    ):
        monkeypatch.delenv(f"BURZBLOG_REMOTE_{name}", raising=False)
    for name, value in values.items():
        monkeypatch.setenv(f"BURZBLOG_REMOTE_{name}", str(value))
    return content


def test_operator_environment_configuration(tmp_path, monkeypatch):
    token_file = secret_file(tmp_path / "secrets", "valid-token")
    url = "https://git.example.invalid/owner/site.git"
    configure(monkeypatch, tmp_path, URL=url, USERNAME=USERNAME, TOKEN_FILE=token_file)
    settings = Settings.from_environment()
    settings.validate()
    assert settings.remote.credential == RemoteCredential(USERNAME, token_file)
    assert "valid-token" not in repr(settings)
    for broken in (
        {"URL": url, "USERNAME": USERNAME},
        {"URL": url, "TOKEN_FILE": token_file},
        {"USERNAME": USERNAME, "TOKEN_FILE": token_file},
        {"URL": url, "TOKEN": "inline-secret"},
        {"URL": url, "PASSWORD": "inline-secret"},
    ):
        configure(monkeypatch, tmp_path, **broken)
        with pytest.raises(ContentError) as failure:
            Settings.from_environment()
        assert "inline-secret" not in str(failure.value)


def test_secret_files_must_live_outside_content_repository_and_state(
    tmp_path, monkeypatch
):
    content = configure(monkeypatch, tmp_path)
    url = "https://git.example.invalid/site.git"
    for directory in (content / "secrets", tmp_path / "state" / "secrets"):
        directory.parent.mkdir(mode=0o700, exist_ok=True)
        token_file = secret_file(directory, "valid-token")
        configure(
            monkeypatch, tmp_path, URL=url, USERNAME=USERNAME, TOKEN_FILE=token_file
        )
        with pytest.raises(ContentError, match="outside content"):
            Settings.from_environment().validate()
    site = tmp_path / "site"
    (site / "content").mkdir(parents=True)
    git(site, "init", "--quiet")
    in_repository = secret_file(site / "secrets", "valid-token")
    settings = Settings(
        site / "content",
        tmp_path / "state",
        remote=Remote("origin", url, RemoteCredential(USERNAME, in_repository)),
    )
    with pytest.raises(ContentError, match="Git repository"):
        settings.validate()


def test_old_git_is_refused_for_credentials(tmp_path, monkeypatch):
    token_file = secret_file(tmp_path / "secrets", "valid-token")
    configure(
        monkeypatch,
        tmp_path,
        URL="https://git.example.invalid/site.git",
        USERNAME=USERNAME,
        TOKEN_FILE=token_file,
    )
    monkeypatch.setattr("burzblog.settings.git_version", lambda: (2, 30))
    with pytest.raises(ContentError, match="2.31"):
        Settings.from_environment().validate()


def test_server_reports_specific_secret_free_setup_errors(
    tmp_path, monkeypatch, capsys
):
    token_file = secret_file(tmp_path / "secrets", "leaky-token-value", 0o644)
    configure(
        monkeypatch,
        tmp_path,
        URL="https://git.example.invalid/site.git",
        USERNAME=USERNAME,
        TOKEN_FILE=token_file,
    )
    monkeypatch.setattr("sys.argv", ["burzblog-server", "serve"])
    assert server.main() == 1
    error = capsys.readouterr().err
    assert "mode 0600" in error and "leaky-token-value" not in error


def test_api_private_remote_flow_redacts_and_rejects_browser_endpoints(
    tmp_path, workspace, private, token
):
    root, content, _ = workspace
    token_file = secret_file(tmp_path / "secrets", token)
    settings = Settings(
        content, tmp_path / "state", remote=remote_for(private, token_file)
    )
    password = "fixture api git password"  # noqa: S105 - temporary fixture
    Authentication(settings.state_directory).initialize(password)
    bodies = []
    with TestClient(create_app(settings), base_url=settings.origin) as client:
        csrf = client.post(
            "/api/login",
            json={"password": password},
            headers={"origin": settings.origin},
        ).json()["csrf"]
        client.headers.update({"origin": settings.origin, "x-csrf-token": csrf})
        state = client.get("/api/git")
        bodies.append(state.text)
        assert state.json()["remote"] == {
            "name": "origin",
            "transport": "https",
            "authenticated": True,
        }
        for action in ("push", "fetch"):
            response = client.post(
                "/api/git", json={"action": action, "branch": "main"}
            )
            bodies.append(response.text)
            assert response.status_code == 200, response.text
        assert git(private.repository, "rev-parse", "main").returncode == 0
        contacted = len(private.requests)
        for hostile in (
            {"action": "fetch", "url": "https://attacker.invalid/x.git"},
            {"action": "push", "branch": "main", "remote": "https://attacker.invalid"},
            {"action": "fetch", "username": "x", "token_file": "/etc/passwd"},
        ):
            response = client.post("/api/git", json=hostile)
            bodies.append(response.text)
            assert response.status_code == 422
        assert len(private.requests) == contacted
        token_file.write_text("revoked-" + token)
        failed = client.post("/api/git", json={"action": "fetch"})
        bodies.append(failed.text)
        assert failed.status_code == 502
        assert failed.json()["error"]["code"] == "remote"
        token_file.chmod(0o644)  # Broken at runtime: refused before Git starts.
        exposed = client.post("/api/git", json={"action": "push", "branch": "main"})
        bodies.append(exposed.text)
        assert exposed.status_code == 502
    for body in bodies:
        assert (
            token not in body
            and str(token_file) not in body
            and private.url not in body
        )
    assert leaked(token, root, settings.state_directory) == []


def test_cli_private_remote_uses_token_file(
    tmp_path, workspace, private, token, monkeypatch, capsys
):
    _, content, _ = workspace
    token_file = secret_file(tmp_path / "secrets", token)
    base = [
        "burzblog",
        "--root",
        str(content),
        "git-push",
        "main",
        "--url",
        private.url,
        "--ca-file",
        str(private.certificate_authority),
    ]
    monkeypatch.setattr("sys.argv", [*base, "--username", USERNAME])
    assert cli.main() == 1
    monkeypatch.setattr(
        "sys.argv", [*base, "--username", USERNAME, "--token-file", str(token_file)]
    )
    assert cli.main() == 0
    token_file.write_text("revoked")
    assert cli.main() == 1
    output = capsys.readouterr()
    assert token not in output.out + output.err
    assert "credentials" in output.err
