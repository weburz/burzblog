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
    web_root: Path | None = None

    @property
    def secure_cookies(self) -> bool:
        """Require HTTPS except for an explicitly loopback development origin."""
        return self.origin.startswith("https://")

    def validate(self) -> None:
        """Complete `serve` validation: origin, storage boundary and Git remote."""
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
        if ":" in origin.hostname:
            # Starlette's host check cannot parse bracketed IPv6 Host headers,
            # so such an origin would reject every request.
            raise ContentError(
                "Use a hostname or IPv4 address in the origin; IPv6 literals are unsupported."
            )
        if origin.scheme == "http" and origin.hostname not in {
            "localhost",
            "127.0.0.1",
        }:
            raise ContentError("Non-loopback deployments require an HTTPS origin.")
        boundary = self.validate_storage()
        if self.remote is not None:
            self._validate_remote(
                self.remote, boundary, self.state_directory.absolute()
            )

    def validate_storage(self) -> Path:
        """Keep authentication state outside content and its Git repository.

        This is all the terminal administrator commands need: they must locate
        state safely, but never depend on Git credentials, network or serving
        configuration. Returns the content/repository boundary.
        """
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
        return repository or self.content_root

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
    def for_administration(cls) -> Settings:
        """Only the paths `init-admin`/`reset-admin` need; Git/serving settings ignored."""
        return cls(*storage_paths())

    @classmethod
    def from_environment(cls) -> Settings:
        """Load explicit server paths and optional operator-selected remote."""
        return cls(
            *storage_paths(),
            os.environ.get("BURZBLOG_ORIGIN", "http://localhost:3000"),
            remote_from_environment(),
            Path(web) if (web := os.environ.get("BURZBLOG_WEB_ROOT")) else None,
        )


def storage_paths() -> tuple[Path, Path]:
    """The configured content root and authentication state directory."""
    root = os.environ.get("BURZBLOG_CONTENT_ROOT")
    state = os.environ.get("BURZBLOG_STATE_DIR")
    if not root or not state:
        raise ContentError("Set BURZBLOG_CONTENT_ROOT and BURZBLOG_STATE_DIR.")
    return Path(root).absolute(), Path(state).absolute()


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
