"""Terminal administrator commands depend only on authentication state safety."""

import os
import sqlite3
from contextlib import closing

import pytest

from burzblog import server
from burzblog.auth import Authentication, AuthenticationError, RateLimitError

OLD = "original fixture password"
NEW = "replacement fixture password"
TOKEN = "secret-token-value-never-printed"  # noqa: S105 - disposable fixture


@pytest.fixture
def deployment(tmp_path, monkeypatch):
    content = tmp_path / "site"
    (content / "posts").mkdir(parents=True)
    state = tmp_path / "state"
    secrets = tmp_path / "secrets"
    secrets.mkdir(mode=0o700)
    for name in list(os.environ):
        if name.startswith("BURZBLOG_"):
            monkeypatch.delenv(name)
    monkeypatch.setenv("BURZBLOG_CONTENT_ROOT", str(content))
    monkeypatch.setenv("BURZBLOG_STATE_DIR", str(state))
    monkeypatch.setenv("BURZBLOG_ORIGIN", "https://blog.example.invalid")
    return content, state, secrets


def broken_git(monkeypatch, secrets, kind):
    """Git/serving configuration that `serve` must refuse."""
    monkeypatch.setenv("BURZBLOG_REMOTE_URL", "https://git.example.invalid/site.git")
    monkeypatch.setenv("BURZBLOG_REMOTE_USERNAME", "site-bot")
    token_file = secrets / "git-token"
    monkeypatch.setenv("BURZBLOG_REMOTE_TOKEN_FILE", str(token_file))
    if kind == "exposed-token":
        token_file.write_text(TOKEN + "\n")
        token_file.chmod(0o644)
    elif kind == "missing-ca":
        token_file.write_text(TOKEN + "\n")
        token_file.chmod(0o600)
        monkeypatch.setenv("BURZBLOG_REMOTE_CA_FILE", str(secrets / "absent-ca.pem"))
    elif kind == "inline-secret":
        monkeypatch.setenv("BURZBLOG_REMOTE_TOKEN", TOKEN)
    elif kind == "unusable-serving":
        monkeypatch.setenv("BURZBLOG_REMOTE_URL", "ssh://git@example.invalid/site.git")
        monkeypatch.setenv("BURZBLOG_ORIGIN", "http://public.example.invalid")
        monkeypatch.setenv("BURZBLOG_WEB_ROOT", str(secrets / "no-build"))
    # kind == "missing-token": the token file simply does not exist.


def command(monkeypatch, name, password=NEW):
    monkeypatch.setattr("sys.argv", ["burzblog-server", name])
    monkeypatch.setattr("getpass.getpass", lambda _: password)
    return server.main()


def assert_no_secrets(text):
    for secret in (OLD, NEW, TOKEN):
        assert secret not in text
    assert "Traceback" not in text


GIT_FAILURES = [
    "missing-token",
    "exposed-token",
    "missing-ca",
    "inline-secret",
    "unusable-serving",
]


@pytest.mark.parametrize("kind", GIT_FAILURES)
def test_reset_admin_ignores_broken_git_and_serving_configuration(
    deployment, monkeypatch, capsys, kind
):
    _, state, secrets = deployment
    authentication = Authentication(state)
    authentication.initialize(OLD)
    sessions = [authentication.login(OLD) for _ in range(3)]
    for _ in range(7):
        with pytest.raises(AuthenticationError):
            authentication.login("wrong fixture password")
    with pytest.raises(RateLimitError):
        authentication.login(OLD)
    broken_git(monkeypatch, secrets, kind)

    assert command(monkeypatch, "reset-admin") == 0

    for session in sessions:
        with pytest.raises(AuthenticationError):
            authentication.verify(session.token)
    with pytest.raises(AuthenticationError):
        authentication.login(OLD)  # throttling was cleared, so this is a real check
    assert authentication.verify(authentication.login(NEW).token)
    output = capsys.readouterr()
    assert "all sessions were signed out" in output.out
    assert_no_secrets(output.out + output.err)


@pytest.mark.parametrize("kind", GIT_FAILURES)
def test_serve_still_refuses_the_same_configuration(
    deployment, monkeypatch, capsys, kind
):
    _, state, secrets = deployment
    Authentication(state).initialize(OLD)
    broken_git(monkeypatch, secrets, kind)
    started = []
    monkeypatch.setattr("uvicorn.run", lambda *args, **kwargs: started.append(1))
    monkeypatch.setattr("sys.argv", ["burzblog-server", "serve"])
    assert server.main() == 1
    assert started == []
    error = capsys.readouterr().err
    assert error.startswith("Setup failed: ")
    assert_no_secrets(error)


@pytest.mark.parametrize("kind", GIT_FAILURES)
def test_init_admin_ignores_broken_git_configuration(
    deployment, monkeypatch, capsys, kind
):
    _, state, secrets = deployment
    broken_git(monkeypatch, secrets, kind)
    assert command(monkeypatch, "init-admin", OLD) == 0
    assert Authentication(state).login(OLD)
    assert command(monkeypatch, "init-admin", NEW) == 1  # never replaces silently
    assert Authentication(state).login(OLD)
    assert_no_secrets(capsys.readouterr().err)


def unsafe_state(content, state, kind):
    """Each case must be refused by both administrator commands."""
    if kind == "inside-content":
        return content / "state"
    if kind == "symlinked":
        real = state.parent / "real-state"
        Authentication(real).initialize(OLD)
        state.symlink_to(real)
        return state
    Authentication(state).initialize(OLD)
    database = state / "authentication.sqlite3"
    if kind == "directory-mode":
        state.chmod(0o755)
    elif kind == "database-mode":
        database.chmod(0o644)
    elif kind == "database-hardlink":
        os.link(database, state.parent / "extra-link")
    elif kind == "database-symlink":
        database.rename(state.parent / "elsewhere.sqlite3")
        database.symlink_to(state.parent / "elsewhere.sqlite3")
    elif kind == "corrupt-database":
        database.write_bytes(b"not a sqlite database " * 64)
    elif kind == "foreign-schema":
        with closing(sqlite3.connect(database)) as connection, connection:
            connection.execute("DROP TABLE administrator")
            connection.execute("CREATE TABLE administrator (id INTEGER, note TEXT)")
    return state


UNSAFE = [
    "inside-content",
    "symlinked",
    "directory-mode",
    "database-mode",
    "database-hardlink",
    "database-symlink",
    "corrupt-database",
    "foreign-schema",
]


@pytest.mark.parametrize("name", ["reset-admin", "init-admin"])
@pytest.mark.parametrize("kind", UNSAFE)
def test_administrator_commands_still_refuse_unsafe_state(
    deployment, monkeypatch, capsys, name, kind
):
    content, state, secrets = deployment
    broken_git(monkeypatch, secrets, "missing-token")
    monkeypatch.setenv("BURZBLOG_STATE_DIR", str(unsafe_state(content, state, kind)))
    assert command(monkeypatch, name) == 1
    error = capsys.readouterr().err
    assert error.startswith("Setup failed: ")
    # Refused because of the state itself, never because of the broken remote.
    assert "Remote" not in error
    assert any(
        word in error for word in ("state", "State", "Authentication", "authentication")
    )
    assert_no_secrets(error)
    if kind == "inside-content":
        assert not (content / "state").exists()


def test_reset_admin_never_creates_state(deployment, monkeypatch, capsys):
    _, state, _ = deployment
    assert command(monkeypatch, "reset-admin") == 1
    assert "init-admin" in capsys.readouterr().err
    assert not state.exists()
    state.mkdir(mode=0o700)
    assert command(monkeypatch, "reset-admin") == 1
    assert not (state / "authentication.sqlite3").exists()


def test_valid_configuration_remains_compatible(deployment, monkeypatch):
    _, state, _ = deployment
    assert command(monkeypatch, "init-admin", OLD) == 0
    assert command(monkeypatch, "reset-admin", NEW) == 0
    assert Authentication(state).login(NEW)
    received = {}
    monkeypatch.setattr("uvicorn.run", lambda app, **kwargs: received.update(kwargs))
    monkeypatch.setattr("sys.argv", ["burzblog-server", "serve"])
    assert server.main() == 0
    assert received["host"] == "127.0.0.1"
