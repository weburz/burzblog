"""Bounded Git subprocesses with isolated environment and no shell."""

import os
import selectors
import shutil
import signal
import subprocess
import time
from pathlib import Path

from burzblog.errors import ContentError


class GitError(ContentError):
    """A repository operation failed without exposing raw diagnostics or secrets."""


def run_git(
    root: Path,
    arguments: list[str],
    timeout: float = 10,
    limit: int = 4 * 1024 * 1024,
    accepted: tuple[int, ...] = (0,),
    protocol: str | None = None,
    git_directory: Path | None = None,
) -> bytes:
    """Execute a fixed Git command, bounding stdout, duration and inherited state."""
    executable = shutil.which("git")
    if executable is None:
        raise GitError("Git is not installed.")
    environment = {
        "PATH": os.defpath,
        "LC_ALL": "C",
        "GIT_CONFIG_NOSYSTEM": "1",
        "GIT_CONFIG_SYSTEM": os.devnull,
        "GIT_CONFIG_GLOBAL": os.devnull,
        "GIT_ATTR_NOSYSTEM": "1",
        "GIT_OPTIONAL_LOCKS": "0",
        "GIT_NO_REPLACE_OBJECTS": "1",
        "GIT_TERMINAL_PROMPT": "0",
        "GIT_PAGER": "cat",
    }
    command = [
        executable,
        f"--git-dir={git_directory or root / '.git'}",
        f"--work-tree={root}",
        "--no-pager",
        "--no-replace-objects",
        "--literal-pathspecs",
        "-c",
        f"core.worktree={root}",
        "-c",
        "core.hooksPath=/dev/null",
        "-c",
        "core.fsmonitor=false",
        "-c",
        "core.untrackedCache=false",
        "-c",
        "core.attributesFile=/dev/null",
        "-c",
        "core.excludesFile=/dev/null",
        "-c",
        "diff.external=",
        "-c",
        "protocol.allow=never",
        "-c",
        "http.followRedirects=false",
        "-c",
        "http.sslVerify=true",
    ]
    if protocol is not None:
        if protocol not in {"file", "https"}:
            raise GitError("Unsupported Git transport.")
        command.extend(["-c", f"protocol.{protocol}.allow=always"])
    command.extend(arguments)
    deadline = time.monotonic() + timeout
    output = bytearray()
    completed = False
    with subprocess.Popen(  # noqa: S603 - argument arrays, trusted Git binary, isolated environment
        command,
        cwd=root,
        env=environment,
        stdin=subprocess.DEVNULL,
        stdout=subprocess.PIPE,
        stderr=subprocess.DEVNULL,
        start_new_session=True,
    ) as process:
        assert process.stdout is not None  # noqa: S101 - PIPE guarantees a stream
        try:
            with selectors.DefaultSelector() as selector:
                selector.register(process.stdout, selectors.EVENT_READ)
                while True:
                    remaining = deadline - time.monotonic()
                    if remaining <= 0 or not selector.select(remaining):
                        raise GitError(
                            "Git operation timed out; inspect repository state."
                        )
                    chunk = os.read(
                        process.stdout.fileno(), min(65536, limit + 1 - len(output))
                    )
                    if not chunk:
                        break
                    output.extend(chunk)
                    if len(output) > limit:
                        raise GitError("Git output exceeds the supported limit.")
            if (
                process.wait(timeout=max(0.001, deadline - time.monotonic()))
                not in accepted
            ):
                raise GitError(
                    "Git operation could not complete; inspect repository state."
                )
            completed = True
        except subprocess.TimeoutExpired:
            raise GitError(
                "Git operation timed out; inspect repository state."
            ) from None
        finally:
            if not completed:
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
            process.wait()
    return bytes(output)
