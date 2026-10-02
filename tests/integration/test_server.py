import pytest

from burzblog import server
from burzblog.auth import Authentication
from burzblog.errors import ContentError
from burzblog.settings import Settings


def test_operator_setup_and_loopback_server(tmp_path, monkeypatch):
    content = tmp_path / "content"
    content.mkdir()
    state = tmp_path / "state"
    monkeypatch.setenv("BURZBLOG_CONTENT_ROOT", str(content))
    monkeypatch.setenv("BURZBLOG_STATE_DIR", str(state))
    monkeypatch.setattr("sys.argv", ["burzblog-server", "init-admin"])
    monkeypatch.setattr("getpass.getpass", lambda _: "fixture password only")
    assert server.main() == 0
    assert server.main() == 1
    monkeypatch.setattr("sys.argv", ["burzblog-server", "reset-admin"])
    monkeypatch.setattr("getpass.getpass", lambda _: "replacement fixture password")
    assert server.main() == 0
    Authentication(state).login("replacement fixture password")
    received = {}
    monkeypatch.setattr("sys.argv", ["burzblog-server", "serve"])
    monkeypatch.setattr("uvicorn.run", lambda app, **kwargs: received.update(kwargs))
    assert server.main() == 0
    assert received["host"] == "127.0.0.1"
    assert received["proxy_headers"] is False and received["access_log"] is False


def test_setup_mismatch_and_missing_configuration(tmp_path, monkeypatch):
    monkeypatch.delenv("BURZBLOG_CONTENT_ROOT", raising=False)
    monkeypatch.delenv("BURZBLOG_STATE_DIR", raising=False)
    with pytest.raises(ContentError):
        Settings.from_environment()
    monkeypatch.setenv("BURZBLOG_CONTENT_ROOT", str(tmp_path))
    monkeypatch.setenv("BURZBLOG_STATE_DIR", str(tmp_path.parent / "state"))
    monkeypatch.setenv("BURZBLOG_REMOTE_URL", "https://example.invalid/repo.git")
    assert Settings.from_environment().remote.url == "https://example.invalid/repo.git"
    values = iter(["fixture password", "different password"])
    monkeypatch.setattr("getpass.getpass", lambda _: next(values))
    monkeypatch.setattr("sys.argv", ["burzblog-server", "init-admin"])
    assert server.main() == 1


def test_password_and_session_bounds(tmp_path):
    auth = Authentication(tmp_path / "state")
    with pytest.raises(ContentError):
        auth.initialize("short")
    with pytest.raises(ContentError):
        auth.login("no administrator")
    with pytest.raises(ContentError):
        auth.verify("x" * 129)
