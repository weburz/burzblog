import pytest

from burzblog.auth import (
    SESSION_SECONDS,
    Authentication,
    AuthenticationError,
    RateLimitError,
)
from burzblog.errors import ContentError
from burzblog.settings import Settings


def test_hash_session_expiry_and_persistent_rate_limit(tmp_path, monkeypatch):
    auth = Authentication(tmp_path / "state")
    password = "only a fixture password"  # noqa: S105 - ephemeral fixture
    auth.initialize(password)
    assert password.encode() not in auth.database.read_bytes()
    assert b"$argon2id$" in auth.database.read_bytes()
    monkeypatch.setattr("burzblog.auth.time.time", lambda: 1000)
    session = auth.login(password)
    assert auth.verify(session.token) == session
    with pytest.raises(AuthenticationError):
        auth.csrf(session, "\u2020")
    monkeypatch.setattr("burzblog.auth.time.time", lambda: 1000 + SESSION_SECONDS)
    with pytest.raises(AuthenticationError):
        auth.verify(session.token)
    for _ in range(10):
        with pytest.raises(AuthenticationError):
            auth.login("wrong")
    with pytest.raises(RateLimitError):
        Authentication(tmp_path / "state").login(password)
    monkeypatch.setattr("burzblog.auth.time.time", lambda: 2000 + SESSION_SECONDS)
    assert auth.login(password).token != session.token
    with pytest.raises(ContentError, match="already exists"):
        auth.initialize(password)


@pytest.mark.parametrize(
    "origin",
    [
        "http://example.com",
        "https://name:pass@example.com",
        "https://example.com/path",
        "https://example.com?x=1",
        "file:///tmp",
        "https://example.com/#part",
    ],
)
def test_unsafe_origin_rejected(tmp_path, origin):
    with pytest.raises(ContentError):
        Settings(tmp_path, tmp_path.parent / "state", origin).validate()


def test_unsafe_state_permissions_and_symlink_rejected(tmp_path):
    state = tmp_path / "state"
    state.mkdir(mode=0o755)
    with pytest.raises(ContentError):
        Authentication(state)
    state.chmod(0o700)
    (state / "authentication.sqlite3").symlink_to(tmp_path / "outside")
    with pytest.raises(ContentError):
        Authentication(state)
