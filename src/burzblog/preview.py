"""Conservative Markdown preview with no raw HTML, images or remote fetches.

Rendering runs in a separate worker process with a memory cap and a time limit:
markdown-it has inputs whose cost grows quadratically (e.g. thousands of
unmatched "["), and one preview must never starve the single service process.
"""

import os
import selectors
import struct
import subprocess
import sys
import time
from threading import Lock

from burzblog.document import parse
from burzblog.errors import ContentError

TIMEOUT_SECONDS = 10.0


class _Worker:
    """One long-lived renderer process, replaced after any failure or timeout."""

    def __init__(self) -> None:
        self._lock = Lock()
        self._process: subprocess.Popen[bytes] | None = None

    def _start(self) -> subprocess.Popen[bytes]:
        if self._process is None or self._process.poll() is not None:
            self._process = subprocess.Popen(
                [sys.executable, "-I", "-m", "burzblog.render"],
                stdin=subprocess.PIPE,
                stdout=subprocess.PIPE,
                stderr=subprocess.DEVNULL,
                env={"PATH": os.defpath},
            )
        return self._process

    def _stop(self) -> None:
        if self._process is not None:
            self._process.kill()
            self._process.wait()
            self._process = None

    def render(self, body: str) -> str:
        """Send one body and wait for its HTML, killing the worker past the limit."""
        with self._lock:
            process = self._start()
            assert process.stdin is not None and process.stdout is not None  # noqa: S101
            data = body.encode("utf-8")
            try:
                process.stdin.write(struct.pack(">Q", len(data)) + data)
                process.stdin.flush()
                reply = self._receive(
                    process.stdout.fileno(), time.monotonic() + TIMEOUT_SECONDS
                )
            except (OSError, ContentError):
                self._stop()
                raise ContentError(
                    "Preview could not be rendered within its time and memory limits. Saving is unaffected."
                ) from None
            return reply.decode("utf-8")

    @staticmethod
    def _receive(fd: int, deadline: float) -> bytes:
        """Read one length-prefixed reply before the deadline."""
        buffer = b""
        expected: int | None = None
        with selectors.DefaultSelector() as selector:
            selector.register(fd, selectors.EVENT_READ)
            while expected is None or len(buffer) < expected + 8:
                remaining = deadline - time.monotonic()
                if remaining <= 0 or not selector.select(remaining):
                    raise ContentError("Preview timed out.")
                chunk = os.read(fd, 1 << 20)
                if not chunk:
                    raise ContentError("Preview worker stopped.")
                buffer += chunk
                if expected is None and len(buffer) >= 8:
                    (expected,) = struct.unpack(">Q", buffer[:8])
        return buffer[8 : 8 + expected]


_worker = _Worker()


def render_preview(markdown: str) -> str:
    """Render and sanitize body text; strip images to prevent tracking requests."""
    return _worker.render(parse(markdown).body)
