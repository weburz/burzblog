"""Operator-configured HTTPS remote credentials; never repository or browser data.

A credential is a non-secret username plus the path of a private secret file.
The secret is read only when a fetch/push starts and is handed to Git through
environment-carried configuration as a URL-scoped Basic authorization header.
It is never stored on objects, written to `.git/config`, placed in a URL or argv,
logged, or included in error messages.
"""

import base64
import os
import re
import stat
from dataclasses import dataclass
from pathlib import Path

from burzblog.git_process import RemoteError

MAX_SECRET_BYTES = 4096
MAX_CERTIFICATE_BYTES = 1024 * 1024
MINIMUM_GIT = (2, 31)  # First release reading GIT_CONFIG_COUNT/KEY/VALUE.
USERNAME = re.compile(r"[\x21-\x39\x3b-\x7e]{1,256}\Z")  # Printable ASCII except ':'.


class CredentialError(RemoteError):
    """Credential configuration is missing, malformed or insufficiently private."""


def read_bounded_file(path: Path, limit: int, private: bool, label: str) -> bytes:
    """Read an operator file without following symlinks or blocking on special files.

    Private files must be owned by the service user and inaccessible to group and
    others. Public trust material may be root-owned but must not be writable by
    anyone else, because a replaced certificate authority enables interception.
    """
    if not path.is_absolute() or os.path.realpath(path) != str(path):
        raise CredentialError(f"{label} must be an absolute path without symlinks.")
    try:
        descriptor = os.open(
            path,
            os.O_RDONLY | os.O_NOFOLLOW | os.O_NOCTTY | os.O_NONBLOCK | os.O_CLOEXEC,
        )
    except OSError:
        raise CredentialError(
            f"{label} is missing or unreadable by the service user."
        ) from None
    try:
        info = os.fstat(descriptor)
        owners = {os.geteuid()} if private else {os.geteuid(), 0}
        if (
            not stat.S_ISREG(info.st_mode)
            or info.st_uid not in owners
            or info.st_mode & (0o077 if private else 0o022)
        ):
            raise CredentialError(
                f"{label} must be a regular file owned by the service user with mode 0600 or 0400."
                if private
                else f"{label} must be a regular file that only its owner can modify."
            )
        data = bytearray()
        while len(data) <= limit:
            chunk = os.read(descriptor, limit + 1 - len(data))
            if not chunk:
                break
            data.extend(chunk)
    finally:
        os.close(descriptor)
    if len(data) > limit:
        raise CredentialError(f"{label} exceeds the supported size.")
    return bytes(data)


@dataclass(frozen=True)
class RemoteCredential:
    """A username and secret-file reference; the secret itself is never retained."""

    username: str
    secret_file: Path

    def validate(self) -> None:
        """Fail at startup rather than at the first fetch/push."""
        self.authorization()

    def authorization(self) -> str:
        """Read the current secret and build an HTTP Basic authorization header.

        Reading per operation allows rotation without restarting. Basic
        username/token authentication is supported by common Git hosts and
        self-hosted forges without any provider-specific API or OAuth flow.
        """
        if not USERNAME.fullmatch(self.username):
            raise CredentialError(
                "Remote username must be 1-256 printable ASCII characters without ':' or spaces."
            )
        raw = read_bounded_file(
            self.secret_file, MAX_SECRET_BYTES, True, "Remote token file"
        )
        try:
            secret = raw.decode("utf-8")
        except UnicodeDecodeError:
            raise CredentialError(
                "Remote token file must contain UTF-8 text."
            ) from None
        secret = secret.removesuffix("\n").removesuffix("\r")
        if not secret or any(ord(char) < 32 or ord(char) == 127 for char in secret):
            raise CredentialError(
                "Remote token file must contain exactly one non-empty line."
            )
        encoded = base64.b64encode(f"{self.username}:{secret}".encode()).decode()
        return f"Authorization: Basic {encoded}"


def validate_certificate_authority(path: Path) -> None:
    """Accept an operator-selected PEM bundle for privately signed HTTPS remotes."""
    data = read_bounded_file(
        path, MAX_CERTIFICATE_BYTES, False, "Certificate authority file"
    )
    if b"-----BEGIN CERTIFICATE-----" not in data:
        raise CredentialError(
            "Certificate authority file must contain PEM certificates."
        )
