"""Explicit operator configuration, separate from content and browser input."""

from __future__ import annotations

import os
from dataclasses import dataclass
from pathlib import Path
from urllib.parse import urlsplit

from burzblog.errors import ContentError
from burzblog.git_domain import GitRepository
from burzblog.git_mutations import Remote


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

    @classmethod
    def from_environment(cls) -> Settings:
        """Load explicit server paths and optional operator-selected remote."""
        root = os.environ.get("BURZBLOG_CONTENT_ROOT")
        state = os.environ.get("BURZBLOG_STATE_DIR")
        if not root or not state:
            raise ContentError("Set BURZBLOG_CONTENT_ROOT and BURZBLOG_STATE_DIR.")
        url = os.environ.get("BURZBLOG_REMOTE_URL")
        return cls(
            Path(root).absolute(),
            Path(state).absolute(),
            os.environ.get("BURZBLOG_ORIGIN", "http://localhost:3000"),
            Remote(os.environ.get("BURZBLOG_REMOTE_NAME", "origin"), url)
            if url
            else None,
        )
