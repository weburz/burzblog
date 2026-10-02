"""Private, durable file journals; never automatically remove captured inodes."""

import json
import os
import stat
import uuid
from collections.abc import Generator
from contextlib import contextmanager

from burzblog.errors import ContentError

RECOVERY_DIRECTORY = ".burzblog-recovery"
DIRECTORY_FLAGS = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW


def write_file(directory: int, name: str, data: bytes, mode: int = 0o600) -> None:
    """Create and sync an exclusive regular file relative to a trusted directory."""
    descriptor = os.open(
        name,
        os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
        mode,
        dir_fd=directory,
    )
    with os.fdopen(descriptor, "wb") as stream:
        stream.write(data)
        stream.flush()
        os.fchmod(stream.fileno(), mode)
        os.fsync(stream.fileno())


@contextmanager
def journal(
    root: int, source: str, destination: str, requested: bytes
) -> Generator[int, None, None]:
    """Persist intent and requested bytes before moving any user-owned inode."""
    try:
        os.mkdir(RECOVERY_DIRECTORY, 0o700, dir_fd=root)
        os.fsync(root)
    except FileExistsError:
        pass
    parent = os.open(RECOVERY_DIRECTORY, DIRECTORY_FLAGS, dir_fd=root)
    try:
        info = os.fstat(parent)
        if info.st_uid != os.geteuid() or stat.S_IMODE(info.st_mode) != 0o700:
            raise ContentError(
                "Recovery directory must be owned by you with mode 0700."
            )
        name = uuid.uuid4().hex
        os.mkdir(name, 0o700, dir_fd=parent)
        os.fsync(parent)
        directory = os.open(name, DIRECTORY_FLAGS, dir_fd=parent)
        try:
            metadata = json.dumps(
                {"source": source, "destination": destination}, sort_keys=True
            )
            write_file(directory, "intent.json", (metadata + "\n").encode())
            write_file(directory, "requested.md", requested)
            os.fsync(directory)
            yield directory
        finally:
            os.close(directory)
    finally:
        os.close(parent)


def capture(parent: int, name: str, directory: int) -> None:
    """Atomically retain the entry actually present, without a read/unlink race."""
    # directory is freshly created and private; original.md cannot preexist.
    os.rename(name, "original.md", src_dir_fd=parent, dst_dir_fd=directory)
    os.fsync(directory)
    os.fsync(parent)
    descriptor = os.open(
        "original.md", os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=directory
    )
    try:
        if not stat.S_ISREG(os.fstat(descriptor).st_mode):
            raise ContentError(
                "Captured entry is not a regular file; inspect recovery."
            )
        os.fsync(descriptor)
    finally:
        os.close(descriptor)
