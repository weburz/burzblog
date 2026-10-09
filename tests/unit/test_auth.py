from concurrent.futures import ThreadPoolExecutor
from threading import Event

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


def test_concurrent_logins_wait_instead_of_running_argon2_in_parallel(
    tmp_path, monkeypatch
):
    from argon2 import PasswordHasher

    auth = Authentication(tmp_path / "state")
    password = "concurrency fixture password"  # noqa: S105 - temporary fixture
    auth.initialize(password)
    release = Event()
    active = []
    overlapped = []
    original = PasswordHasher.verify

    def held_verify(hasher, encoded, candidate):
        active.append(1)
        overlapped.append(len(active) > 1)
        try:
            assert release.wait(timeout=5)
            return original(hasher, encoded, candidate)
        finally:
            active.pop()

    monkeypatch.setattr(PasswordHasher, "verify", held_verify)
    with ThreadPoolExecutor(max_workers=2) as executor:
        first = executor.submit(auth.login, password)
        second = executor.submit(auth.login, password)
        release.set()
        assert first.result(timeout=10).token != second.result(timeout=10).token
    assert overlapped == [False, False]


def test_a_stuck_verification_still_bounds_waiting_logins(tmp_path):
    auth = Authentication(tmp_path / "state")
    password = "concurrency fixture password"  # noqa: S105 - temporary fixture
    auth.initialize(password)
    auth.hash_wait = 0.05
    assert auth.hash_slot.acquire(blocking=False)
    try:
        with pytest.raises(RateLimitError):
            auth.login(password)
    finally:
        auth.hash_slot.release()


def test_reset_replaces_password_revokes_sessions_and_clears_lockout(tmp_path):
    auth = Authentication(tmp_path / "state")
    with pytest.raises(ContentError, match="init-admin"):
        auth.reset("replacement fixture password")
    old = "original fixture password"
    new = "replacement fixture password"
    auth.initialize(old)
    sessions = [auth.login(old) for _ in range(3)]
    for _ in range(7):
        with pytest.raises(AuthenticationError):
            auth.login("wrong fixture password")
    with pytest.raises(RateLimitError):
        auth.login(old)
    with pytest.raises(ContentError):
        auth.reset("short")
    auth.reset(new)
    for session in sessions:
        with pytest.raises(AuthenticationError):
            auth.verify(session.token)
    with pytest.raises(AuthenticationError):
        auth.login(old)
    assert auth.verify(auth.login(new).token)
    assert new.encode() not in auth.database.read_bytes()
    assert (
        auth.database.stat().st_mode & 0o777,
        auth.database.parent.stat().st_mode & 0o777,
    ) == (0o600, 0o700)
