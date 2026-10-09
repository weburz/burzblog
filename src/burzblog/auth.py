"""Single-administrator Argon2 authentication with opaque server-side sessions."""

import hashlib
import hmac
import os
import secrets
import sqlite3
import stat
import time
from collections.abc import Generator
from contextlib import contextmanager
from dataclasses import dataclass
from pathlib import Path
from threading import BoundedSemaphore

from argon2 import PasswordHasher
from argon2.exceptions import VerificationError

from burzblog.errors import ContentError

SESSION_SECONDS = 12 * 60 * 60
SCHEMA = {
    "administrator": ("id", "password_hash"),
    "sessions": ("token_hash", "csrf", "expires"),
    "attempts": ("id", "started", "count"),
}


class AuthenticationError(ContentError):
    """Authentication was absent, expired or invalid."""


class RateLimitError(AuthenticationError):
    """A bounded login window has exhausted its attempts."""


class CredentialsError(AuthenticationError):
    """A login attempt supplied a password that did not verify."""


class OriginError(AuthenticationError):
    """A mutation arrived from a page that is not the configured origin."""


@dataclass(frozen=True)
class Session:
    """An opaque cookie credential and separate CSRF token."""

    token: str
    csrf: str


def digest(token: str) -> str:
    """Store only a one-way digest of high-entropy session credentials."""
    return hashlib.sha256(token.encode()).hexdigest()


class Authentication:
    """Persist operational authentication state; never article content."""

    def __init__(self, directory: Path, create: bool = True) -> None:
        """Require a private operator-owned state directory and regular SQLite file.

        With `create=False` (administrator recovery) nothing is created: a
        mistyped or missing state path fails instead of starting empty state.
        """
        if not create and not os.path.lexists(directory / "authentication.sqlite3"):
            raise ContentError(
                "No authentication state exists in the configured state directory; run init-admin first."
            )
        directory.mkdir(mode=0o700, exist_ok=True)
        info = directory.lstat()
        if (
            directory.resolve() != directory.absolute()
            or not stat.S_ISDIR(info.st_mode)
            or info.st_uid != os.geteuid()
            or stat.S_IMODE(info.st_mode) != 0o700
        ):
            raise ContentError(
                "State directory must be privately owned with mode 0700."
            )
        self.database = directory / "authentication.sqlite3"
        try:
            descriptor = os.open(
                self.database,
                os.O_CREAT | os.O_EXCL | os.O_WRONLY | os.O_NOFOLLOW,
                0o600,
            )
            os.close(descriptor)
        except FileExistsError:
            pass
        info = self.database.lstat()
        if (
            not stat.S_ISREG(info.st_mode)
            or info.st_nlink != 1
            or info.st_uid != os.geteuid()
            or stat.S_IMODE(info.st_mode) != 0o600
        ):
            raise ContentError(
                "Authentication database must be a private regular file."
            )
        self.hasher = PasswordHasher()
        self.hash_slot = BoundedSemaphore(1)
        # Seconds a login waits for another verification before being refused.
        self.hash_wait = 10.0
        with self.connection() as connection:
            connection.executescript("""
                CREATE TABLE IF NOT EXISTS administrator (id INTEGER PRIMARY KEY CHECK (id = 1), password_hash TEXT NOT NULL);
                CREATE TABLE IF NOT EXISTS sessions (token_hash TEXT PRIMARY KEY, csrf TEXT NOT NULL, expires INTEGER NOT NULL);
                CREATE TABLE IF NOT EXISTS attempts (id INTEGER PRIMARY KEY CHECK (id = 1), started INTEGER NOT NULL, count INTEGER NOT NULL CHECK (count >= 0));
            """)
            # CREATE ... IF NOT EXISTS never repairs an existing table: refuse
            # state whose structure is not exactly what BurzBlog created.
            for table, columns in SCHEMA.items():  # Constant table names only.
                found = tuple(
                    row[1] for row in connection.execute(f"PRAGMA table_info({table})")
                )
                if found != columns:
                    raise ContentError(
                        "Authentication database has an unexpected structure; restore the state directory from backup."
                    )

    @contextmanager
    def connection(self) -> Generator[sqlite3.Connection, None, None]:
        """Use short explicit transactions and always close database connections."""
        connection = sqlite3.connect(self.database, timeout=5)
        try:
            with connection:
                yield connection
        finally:
            connection.close()

    def initialize(self, password: str) -> None:
        """Create the sole administrator once; never silently replace an existing hash."""
        if not 12 <= len(password) <= 1024:
            raise ContentError("Use a password between 12 and 1024 characters.")
        password_hash = self.hasher.hash(password)
        try:
            with self.connection() as connection:
                connection.execute(
                    "INSERT INTO administrator VALUES (1, ?)", (password_hash,)
                )
        except sqlite3.IntegrityError:
            raise ContentError(
                "Administrator already exists; no password was changed."
            ) from None

    def reset(self, password: str) -> None:
        """Replace the existing administrator password and revoke every session.

        Operator terminal only; also clears the login-attempt window so a locked
        out operator can sign in immediately with the new password.
        """
        if not 12 <= len(password) <= 1024:
            raise ContentError("Use a password between 12 and 1024 characters.")
        password_hash = self.hasher.hash(password)
        with self.connection() as connection:
            connection.execute("BEGIN IMMEDIATE")
            changed = connection.execute(
                "UPDATE administrator SET password_hash=? WHERE id=1", (password_hash,)
            ).rowcount
            if changed != 1:
                raise ContentError("No administrator exists yet; run init-admin.")
            connection.execute("DELETE FROM sessions")
            connection.execute("DELETE FROM attempts")

    def login(self, password: str) -> Session:
        """Rate-limit globally before hashing; create a fresh expiring opaque session."""
        now = int(time.time())
        with self.connection() as connection:
            connection.execute("BEGIN IMMEDIATE")
            attempt: tuple[int, int] | None = connection.execute(
                "SELECT started, count FROM attempts WHERE id=1"
            ).fetchone()
            started, count = attempt if attempt and now - attempt[0] < 600 else (now, 0)
            if count >= 10:
                raise RateLimitError(
                    "Too many login attempts. Try again in ten minutes."
                )
            connection.execute(
                "INSERT OR REPLACE INTO attempts VALUES (1, ?, ?)", (started, count + 1)
            )
            row: tuple[str] | None = connection.execute(
                "SELECT password_hash FROM administrator WHERE id=1"
            ).fetchone()
        if row is None or not 1 <= len(password) <= 1024:
            raise CredentialsError("Login failed.")
        verified_hash = self._verified_hash(row[0], password)
        session = Session(secrets.token_urlsafe(32), secrets.token_urlsafe(32))
        with self.connection() as connection:
            connection.execute("DELETE FROM sessions WHERE expires <= ?", (now,))
            # Bound stored sessions even across repeated authorized logins.
            connection.execute(
                "DELETE FROM sessions WHERE token_hash NOT IN (SELECT token_hash FROM sessions ORDER BY expires DESC LIMIT 19)"
            )
            connection.execute(
                "INSERT INTO sessions VALUES (?, ?, ?)",
                (digest(session.token), session.csrf, now + SESSION_SECONDS),
            )
            if verified_hash != row[0]:
                connection.execute(
                    "UPDATE administrator SET password_hash=? WHERE id=1",
                    (verified_hash,),
                )
        return session

    def _verified_hash(self, password_hash: str, password: str) -> str:
        """Bound concurrent Argon2 memory use without weakening hash parameters."""
        # A double-click or a second tab waits for the verification in progress
        # instead of being refused; memory stays bounded to one verification.
        if not self.hash_slot.acquire(timeout=self.hash_wait):
            raise RateLimitError("Another login is being verified. Try again shortly.")
        try:
            try:
                self.hasher.verify(password_hash, password)
            except VerificationError:
                raise CredentialsError("Login failed.") from None
            return (
                self.hasher.hash(password)
                if self.hasher.check_needs_rehash(password_hash)
                else password_hash
            )
        finally:
            self.hash_slot.release()

    def verify(self, token: str | None) -> Session:
        """Validate an unexpired credential without returning the stored digest."""
        if token is None or len(token) > 128:
            raise AuthenticationError("Please log in.")
        with self.connection() as connection:
            row: tuple[str] | None = connection.execute(
                "SELECT csrf FROM sessions WHERE token_hash=? AND expires>?",
                (digest(token), int(time.time())),
            ).fetchone()
        if row is None:
            raise AuthenticationError("Please log in.")
        return Session(token, row[0])

    def csrf(self, session: Session, token: str | None) -> None:
        """Require a session-bound CSRF token for every authenticated mutation."""
        if (
            not token
            or len(token) > 128
            or not token.isascii()
            or not hmac.compare_digest(session.csrf, token)
        ):
            raise AuthenticationError(
                "Request verification failed. Reload and try again."
            )

    def logout(self, token: str) -> None:
        """Revoke the current server-side session immediately."""
        with self.connection() as connection:
            connection.execute(
                "DELETE FROM sessions WHERE token_hash=?", (digest(token),)
            )
