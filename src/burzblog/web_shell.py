"""Serve the prebuilt browser application from the API process, same-origin.

The Nuxt interface is a static single-page application (`ssr: false`), so
production needs no Node.js runtime or second proxy hop: this process returns
the generated shell and its fingerprinted assets, and `/api/` stays on the same
origin. Nothing here reads content or exposes authenticated data.
"""

import base64
import hashlib
import os
import re
from pathlib import Path

from starlette.responses import Response
from starlette.staticfiles import StaticFiles
from starlette.types import Scope

from burzblog.errors import ContentError

INLINE_SCRIPT = re.compile(rb"<script(?![^>]*\bsrc=)([^>]*)>(.*?)</script>", re.DOTALL)
MAX_SHELL_BYTES = 1024 * 1024


class WebShell:
    """The generated `index.html` plus a Content-Security-Policy derived from it."""

    def __init__(self, root: Path) -> None:
        """Load the shell once; hash its inline bootstrap scripts for the CSP."""
        index = root / "index.html"
        if (
            not root.is_absolute()
            or not (root / "_nuxt").is_dir()
            or not index.is_file()
            or index.stat().st_size > MAX_SHELL_BYTES
        ):
            raise ContentError(
                "Web root must be an absolute generated browser build containing index.html and _nuxt/."
            )
        self.root = root
        self.index = index.read_bytes()
        hashes = sorted(
            {
                "'sha256-"
                + base64.b64encode(hashlib.sha256(body).digest()).decode()
                + "'"
                for attributes, body in INLINE_SCRIPT.findall(self.index)
                if b"application/json" not in attributes
            }
        )
        # Inline styles stay allowed: the sandboxed preview iframe is srcdoc and
        # inherits this policy, and it carries its own fixed stylesheet.
        self.policy = "; ".join(
            (
                "default-src 'none'",
                "script-src 'self' " + " ".join(hashes),
                "style-src 'self' 'unsafe-inline'",
                "font-src 'self' data:",
                "img-src 'self' data:",
                "connect-src 'self'",
                "base-uri 'none'",
                "form-action 'self'",
                "frame-ancestors 'none'",
                "object-src 'none'",
            )
        )

    def response(self) -> Response:
        """Return the shell; revalidate it so upgrades take effect immediately."""
        return Response(
            self.index,
            media_type="text/html; charset=utf-8",
            headers={
                "cache-control": "no-cache",
                "content-security-policy": self.policy,
            },
        )


class FingerprintedAssets(StaticFiles):
    """Content-hashed build assets: cache forever, never follow symlinks."""

    def file_response(
        self,
        full_path: str | os.PathLike[str],
        stat_result: os.stat_result,
        scope: Scope,
        status_code: int = 200,
    ) -> Response:
        """Mark immutable so browsers skip revalidation of hashed filenames."""
        response = super().file_response(full_path, stat_result, scope, status_code)
        response.headers["cache-control"] = "public, max-age=31536000, immutable"
        return response
