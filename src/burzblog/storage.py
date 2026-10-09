"""POSIX content storage bounded by a privately controlled directory tree."""

import fcntl
import hashlib
import os
import re
import stat
import uuid
from collections.abc import Generator
from contextlib import contextmanager
from dataclasses import dataclass
from pathlib import Path

from burzblog.document import MAX_BYTES, Document, parse, serialize
from burzblog.errors import ConflictError, ContentError, ExistsError, RecoveryError
from burzblog.recovery import capture, journal, write_file

SEGMENT = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,127}\Z")
DIRECTORY_FLAGS = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW


@dataclass(frozen=True)
class Snapshot:
    """A document and revision token for optimistic concurrency checks."""

    document: Document
    revision: str


def _parts(path: str, archive: bool = False) -> list[str]:
    """Validate lexical paths before any filesystem access."""
    parts = path.split("/")
    if len(parts) > 32 or len(path) > 2048 or not path.endswith(".md"):
        raise ContentError("Use a relative .md path within the content root.")
    for index, part in enumerate(parts):
        if archive and index == 0 and part == ".archive":
            continue
        if not SEGMENT.fullmatch(part) or part in (".", ".."):
            raise ContentError("Unsupported content path.")
    return parts


def _revision(data: bytes) -> str:
    """Hash actual file bytes, not parsed metadata."""
    return hashlib.sha256(data).hexdigest()


def validate_content_path(path: str) -> None:
    """Validate a caller's content-relative filename without touching disk."""
    _parts(path)


class ContentStore:
    """Read fresh disk state and serialize application mutations with flock."""

    def __init__(self, root: str | Path) -> None:
        """Configure an existing root; do not create or normalize user content."""
        self.root = Path(os.path.abspath(root))

    @contextmanager
    def _root(self, write: bool = False) -> Generator[int, None, None]:
        """Open every absolute component without following symlinks."""
        fd = os.open("/", DIRECTORY_FLAGS)
        try:
            for part in self.root.parts[1:]:
                child = os.open(part, DIRECTORY_FLAGS, dir_fd=fd)
                os.close(fd)
                fd = child
            info = os.fstat(fd)
            if info.st_uid != os.geteuid() or info.st_mode & 0o022:
                raise ContentError(
                    "Content root must be owned by you and not group/world writable."
                )
            if write:
                fcntl.flock(fd, fcntl.LOCK_EX)
            yield fd
        finally:
            os.close(fd)

    @contextmanager
    def _parent(self, root: int, parts: list[str]) -> Generator[int, None, None]:
        """Anchor traversal to open directories and reject unsafe parents."""
        fd = os.dup(root)
        try:
            for part in parts[:-1]:
                child = os.open(part, DIRECTORY_FLAGS, dir_fd=fd)
                os.close(fd)
                fd = child
                info = os.fstat(fd)
                if info.st_uid != os.geteuid() or info.st_mode & 0o022:
                    raise ContentError(
                        "Content directories must be privately controlled."
                    )
            yield fd
        finally:
            os.close(fd)

    def _bytes(self, parent: int, name: str) -> tuple[bytes, os.stat_result]:
        """Read bounded regular files; nonblocking prevents FIFO hangs."""
        fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=parent)
        try:
            info = os.fstat(fd)
            if (
                not stat.S_ISREG(info.st_mode)
                or info.st_nlink != 1
                or info.st_uid != os.geteuid()
                or info.st_gid != os.getegid()
                or info.st_mode & 0o7022
            ):
                raise ContentError(
                    "Content must be a privately owned regular file with one link."
                )
            if info.st_size > MAX_BYTES:
                raise ContentError("Document exceeds 2 MiB.")
            with os.fdopen(os.dup(fd), "rb") as stream:
                data = stream.read(MAX_BYTES + 1)
            if len(data) > MAX_BYTES:
                raise ContentError("Document exceeds 2 MiB.")
            after = os.fstat(fd)
            if (info.st_mtime_ns, info.st_ctime_ns, info.st_size) != (
                after.st_mtime_ns,
                after.st_ctime_ns,
                after.st_size,
            ):
                raise ConflictError("File changed during read; read it again.")
            return data, info
        finally:
            os.close(fd)

    def read(self, path: str) -> Snapshot:
        """Read from disk every time so external edits are immediately visible."""
        parts = _parts(path)
        with self._root() as root, self._parent(root, parts) as parent:
            data, _ = self._bytes(parent, parts[-1])
        try:
            document = parse(data.decode("utf-8"))
        except UnicodeDecodeError:
            raise ContentError("Content must be UTF-8.") from None
        return Snapshot(document, _revision(data))

    def discover(self) -> list[str]:
        """List visible supported files, bounded to 10,000 directory entries."""
        found: list[str] = []
        budget = [10000]

        def visit(fd: int, prefix: str, depth: int) -> None:
            if depth > 32:
                raise ContentError("Discovery exceeds 32 directory levels.")
            with os.scandir(fd) as entries:
                for entry in entries:
                    budget[0] -= 1
                    if budget[0] < 0:
                        raise ContentError("Discovery exceeds 10,000 entries.")
                    if not SEGMENT.fullmatch(entry.name):
                        continue
                    path = prefix + entry.name
                    if entry.is_dir(follow_symlinks=False):
                        with self._parent(fd, [entry.name, "placeholder.md"]) as child:
                            visit(child, path + "/", depth + 1)
                    elif entry.is_file(follow_symlinks=False) and path.endswith(".md"):
                        _parts(path)
                        found.append(path)

        with self._root() as root:
            visit(root, "", 0)
        return sorted(found)

    def create(self, path: str, document: Document) -> str:
        """Publish a new file without overwriting any existing directory entry."""
        return self._write(path, document, None)

    def save(self, path: str, document: Document, revision: str) -> str:
        """Retain the prior inode before publishing; no-op saves do not touch disk."""
        return self._write(path, document, revision)

    def _write(self, path: str, document: Document, revision: str | None) -> str:
        """Stage, sync and publish content with cleanup on pre-publication failures."""
        parts = _parts(path)
        data = serialize(document).encode("utf-8")
        with self._root(write=True) as root, self._parent(root, parts) as parent:
            mode = 0o600
            if revision is not None:
                previous, info = self._bytes(parent, parts[-1])
                if _revision(previous) != revision:
                    raise ConflictError(
                        "File changed externally; read it again before saving."
                    )
                if previous == data:
                    return revision
                mode = stat.S_IMODE(info.st_mode)
            temporary = f".burzblog-{uuid.uuid4().hex}.tmp"
            fd = os.open(
                temporary,
                os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
                0o600,
                dir_fd=parent,
            )
            try:
                with os.fdopen(fd, "wb") as stream:
                    stream.write(data)
                    stream.flush()
                    os.fchmod(stream.fileno(), mode)
                    os.fsync(stream.fileno())
                if revision is None:
                    try:
                        os.link(
                            temporary,
                            parts[-1],
                            src_dir_fd=parent,
                            dst_dir_fd=parent,
                            follow_symlinks=False,
                        )
                    except FileExistsError:
                        raise ExistsError(
                            "Destination already exists; nothing was overwritten."
                        ) from None
                else:
                    current, info = self._bytes(parent, parts[-1])
                    if _revision(current) != revision:
                        raise ConflictError(
                            "File changed externally; nothing was overwritten."
                        )
                    self._publish(
                        root,
                        parent,
                        parts[-1],
                        parent,
                        parts[-1],
                        path,
                        path,
                        current,
                        data,
                        revision,
                        info,
                        mode,
                    )
                os.fsync(parent)
            finally:
                try:
                    os.unlink(temporary, dir_fd=parent)
                except FileNotFoundError:
                    pass
            os.fsync(parent)
        return _revision(data)

    def _publish(
        self,
        root: int,
        source_fd: int,
        source: str,
        target_fd: int,
        target: str,
        source_path: str,
        destination_path: str,
        previous: bytes,
        requested: bytes,
        revision: str,
        original_info: os.stat_result,
        mode: int,
    ) -> None:
        """Capture the exact prior inode, then publish without replacing any entry."""
        with journal(root, source_path, destination_path, requested) as recovery:
            # A source recovery copy is also useful if the entry disappears externally.
            write_file(recovery, "observed.md", previous)
            write_file(recovery, "publish.md", requested, mode)
            os.fsync(recovery)
            try:
                capture(source_fd, source, recovery)
                actual, info = self._bytes(recovery, "original.md")
                if _revision(actual) != revision or (info.st_dev, info.st_ino) != (
                    original_info.st_dev,
                    original_info.st_ino,
                ):
                    raise RecoveryError(
                        "External change preserved in .burzblog-recovery; review before retrying."
                    )
                # No replace and no source unlink: a concurrent creator wins safely.
                os.link(
                    "publish.md",
                    target,
                    src_dir_fd=recovery,
                    dst_dir_fd=target_fd,
                    follow_symlinks=False,
                )
                os.fsync(target_fd)
                os.unlink("publish.md", dir_fd=recovery)
                write_file(
                    recovery,
                    "complete",
                    b"Published; original.md retained for late external writes.\n",
                )
                os.fsync(recovery)
            except (OSError, ContentError):
                raise RecoveryError(
                    "Operation needs recovery; inspect .burzblog-recovery before retrying. No captured version was deleted."
                ) from None

    def rename(self, source: str, destination: str, revision: str) -> None:
        """Relocate a file without changing its metadata or overwriting a target."""
        self._move(source, destination, revision)

    def archive(self, source: str, revision: str) -> str:
        """Move into an existing .archive directory; never permanently delete."""
        basename = _parts(source)[-1][:-3][:90]
        destination = f".archive/{uuid.uuid4().hex}-{basename}.md"
        self._move(source, destination, revision, archive=True)
        return destination

    def _move(
        self, source: str, destination: str, revision: str, archive: bool = False
    ) -> None:
        """Retain the exact source inode before publishing an independent destination."""
        src = _parts(source)
        dst = _parts(destination, archive=archive)
        with (
            self._root(write=True) as root,
            self._parent(root, src) as source_fd,
            self._parent(root, dst) as target_fd,
        ):
            data, info = self._bytes(source_fd, src[-1])
            if _revision(data) != revision:
                raise ConflictError(
                    "File changed externally; read it again before moving."
                )
            try:
                os.stat(dst[-1], dir_fd=target_fd, follow_symlinks=False)
            except FileNotFoundError:
                pass
            else:
                raise ExistsError(
                    "Destination already exists; nothing was overwritten."
                )
            self._publish(
                root,
                source_fd,
                src[-1],
                target_fd,
                dst[-1],
                source,
                destination,
                data,
                data,
                revision,
                info,
                stat.S_IMODE(info.st_mode),
            )
