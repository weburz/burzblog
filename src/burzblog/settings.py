"""Explicit operator configuration, separate from content and browser input."""

from __future__ import annotations

import os
from dataclasses import dataclass
from pathlib import Path
from urllib.parse import urlsplit

from burzblog.errors import ContentError
from burzblog.git_credentials import (
    MINIMUM_GIT,
    RemoteCredential,
    validate_certificate_authority,
)
from burzblog.git_domain import GitRepository
from burzblog.git_mutations import Remote
from burzblog.git_process import git_version

INLINE_SECRETS = ("BURZBLOG_REMOTE_TOKEN", "BURZBLOG_REMOTE_PASSWORD")


@dataclass(frozen=True)
class Settings:
    """Single-project server configuration; no arbitrary browser filesystem roots."""

    content_root: Path
    state_directory: Path
    origin: str = "http://localhost:3000"
    remote: Remote | None = None

    @property
    def secure_cookies(self) -> bool:
        """Require HTTPS except for an explicitly loopback development origin."""
        return self.origin.startswith("https://")

    def validate(self) -> None:
        """Refuse secrets inside the content/repository and unsupported origins."""
        origin = urlsplit(self.origin)
        if (
            origin.scheme not in {"http", "https"}
            or not origin.hostname
            or origin.username
            or origin.password
            or origin.path
            or origin.query
            or origin.fragment
        ):
            raise ContentError("Configure one exact HTTP(S) origin without a path.")
        if origin.scheme == "http" and origin.hostname not in {
            "localhost",
            "127.0.0.1",
            "::1",
        }:
            raise ContentError("Non-loopback deployments require an HTTPS origin.")
        if (
            not self.content_root.is_dir()
            or self.content_root.resolve() != self.content_root.absolute()
        ):
            raise ContentError("Configure an existing non-symlink content root.")
        state = self.state_directory.absolute()
        repository = GitRepository(self.content_root).discover()
        if state.is_relative_to(repository or self.content_root.absolute()):
            raise ContentError(
                "Operational state must be outside content and its Git repository."
            )
        if state.resolve() != state:
            raise ContentError("Operational state cannot use symlinked paths.")
        if self.remote is not None:
            self._validate_remote(self.remote, repository or self.content_root, state)

    @staticmethod
    def _validate_remote(remote: Remote, repository: Path, state: Path) -> None:
        """Check endpoint shape and keep secret/trust files apart from other state."""
        remote.transport()
        for path in (
            remote.credential.secret_file if remote.credential else None,
            remote.certificate_authority,
        ):
            if path is not None and any(
                path.absolute().is_relative_to(boundary)
                for boundary in (repository.absolute(), state)
            ):
                raise ContentError(
                    "Remote credential files must be outside content, its Git repository and authentication state."
                )
        if remote.certificate_authority is not None:
            validate_certificate_authority(remote.certificate_authority)
        if remote.credential is not None:
            remote.credential.validate()
            if git_version() < MINIMUM_GIT:
                raise ContentError("Remote credentials require Git 2.31 or newer.")

    @classmethod
    def from_environment(cls) -> Settings:
        """Load explicit server paths and optional operator-selected remote."""
        root = os.environ.get("BURZBLOG_CONTENT_ROOT")
        state = os.environ.get("BURZBLOG_STATE_DIR")
        if not root or not state:
            raise ContentError("Set BURZBLOG_CONTENT_ROOT and BURZBLOG_STATE_DIR.")
        return cls(
            Path(root).absolute(),
            Path(state).absolute(),
            os.environ.get("BURZBLOG_ORIGIN", "http://localhost:3000"),
            remote_from_environment(),
        )


def remote_from_environment() -> Remote | None:
    """Read the operator remote; secrets are referenced by file, never inlined."""
    if any(name in os.environ for name in INLINE_SECRETS):
        raise ContentError(
            "Do not put Git secrets in the environment; set BURZBLOG_REMOTE_TOKEN_FILE to a private file."
        )
    url = os.environ.get("BURZBLOG_REMOTE_URL")
    username = os.environ.get("BURZBLOG_REMOTE_USERNAME")
    token_file = os.environ.get("BURZBLOG_REMOTE_TOKEN_FILE")
    authority = os.environ.get("BURZBLOG_REMOTE_CA_FILE")
    if bool(username) != bool(token_file):
        raise ContentError(
            "Set both BURZBLOG_REMOTE_USERNAME and BURZBLOG_REMOTE_TOKEN_FILE, or neither."
        )
    if not url:
        if username or authority:
            raise ContentError("Remote credentials require BURZBLOG_REMOTE_URL.")
        return None
    return Remote(
        os.environ.get("BURZBLOG_REMOTE_NAME", "origin"),
        url,
        RemoteCredential(username, Path(token_file))
        if username and token_file
        else None,
        Path(authority) if authority else None,
    )
