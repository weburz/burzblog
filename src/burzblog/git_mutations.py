"""Explicit Git changes with scoped content and recoverable working-tree transitions."""

from __future__ import annotations

import fcntl
import json
import os
import re
import stat
from collections.abc import Callable
from dataclasses import dataclass
from functools import wraps
from pathlib import Path
from typing import Concatenate
from urllib.parse import urlsplit

from burzblog.document import MAX_BYTES, Document, parse, serialize
from burzblog.errors import ConflictError
from burzblog.git_credentials import RemoteCredential, validate_certificate_authority
from burzblog.git_domain import SAFE_KEYS, SAFE_SECTION, GitRepository, RepositoryState
from burzblog.git_process import GitError, RemoteError, run_git
from burzblog.storage import ContentStore, validate_content_path

BRANCH = re.compile(r"[A-Za-z0-9][A-Za-z0-9_/-]{0,127}\Z")
REMOTE_NAME = re.compile(r"[A-Za-z0-9][A-Za-z0-9_-]{0,63}\Z")
UNREACHABLE = (
    "Could not reach the configured remote, or it refused the request or credentials. "
    "Local content was not changed."
)


def serialized[Result, **Parameters](
    operation: Callable[Concatenate[GitChanges, Parameters], Result],
) -> Callable[Concatenate[GitChanges, Parameters], Result]:
    """Serialize application Git writers on the existing metadata directory inode."""

    @wraps(operation)
    def invoke(
        self: GitChanges, /, *args: Parameters.args, **kwargs: Parameters.kwargs
    ) -> Result:
        root = self.repository.checked_root()
        descriptor = os.open(
            root / ".git", os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW
        )
        try:
            fcntl.flock(descriptor, fcntl.LOCK_EX)
            return operation(self, *args, **kwargs)
        finally:
            os.close(descriptor)

    return invoke


def branch_name(name: str) -> str:
    """Accept simple portable branch names, rejecting revisions and options."""
    if not BRANCH.fullmatch(name) or "//" in name or name.endswith("/"):
        raise GitError(
            "Use a simple branch name with letters, digits, hyphens and slashes."
        )
    return name


@dataclass(frozen=True)
class Remote:
    """Operator-provided endpoint; never inferred from potentially hostile repo URLs."""

    name: str
    url: str
    credential: RemoteCredential | None = None
    certificate_authority: Path | None = None

    def transport(self) -> str:
        """Validate the configured shape without touching the remote or secrets."""
        if not REMOTE_NAME.fullmatch(self.name):
            raise GitError("Invalid remote name.")
        if Path(self.url).is_absolute():
            if self.credential or self.certificate_authority:
                raise GitError(
                    "Credentials and certificate authorities apply only to HTTPS remotes."
                )
            return "file"
        parsed = urlsplit(self.url)
        if (
            parsed.scheme != "https"
            or not parsed.hostname
            or parsed.username
            or parsed.password
            or parsed.query
            or parsed.fragment
            or any(ord(char) < 33 or ord(char) == 127 for char in self.url)
        ):
            raise GitError(
                "Remote must be an operator-approved HTTPS URL without embedded credentials."
            )
        return "https"

    def protocol(self) -> str:
        """Allow an explicit local fixture path or an HTTPS endpoint."""
        transport = self.transport()
        if transport == "file":
            path = Path(self.url)
            if path.resolve() != path or not path.is_dir():
                raise GitError(
                    "Local remote must be an existing non-symlink directory."
                )
            validate_local_remote(path)
        return transport

    def configuration(self) -> dict[str, str]:
        """Per-command transport settings; carried by environment, never argv/config files."""
        configuration: dict[str, str] = {}
        if self.certificate_authority is not None:
            validate_certificate_authority(self.certificate_authority)
            configuration["http.sslCAInfo"] = str(self.certificate_authority)
        if self.credential is not None:
            # URL-scoped so the header is attached only to this exact endpoint.
            configuration[f"http.{self.url}.extraHeader"] = (
                self.credential.authorization()
            )
        return configuration


def validate_local_remote(path: Path) -> None:
    """Refuse local receiver code/configuration before allowing file transport."""
    root_info = path.stat()
    if root_info.st_uid != os.geteuid() or root_info.st_mode & 0o022:
        raise GitError("Local remote root must be privately owned.")
    count = 0
    for directory, directories, files in os.walk(path, followlinks=False):
        for entry in (*directories, *files):
            count += 1
            info = (Path(directory) / entry).lstat()
            if (
                count > 100000
                or stat.S_ISLNK(info.st_mode)
                or info.st_uid != os.geteuid()
                or info.st_mode & 0o022
            ):
                raise GitError("Local remote metadata is not privately controlled.")
    hooks = path / "hooks"
    if hooks.exists() and any(
        not hook.name.endswith(".sample") and os.access(hook, os.X_OK)
        for hook in hooks.iterdir()
    ):
        raise GitError("Local remote has active hooks; use external Git tooling.")
    config = path / "config"
    if (
        not config.is_file()
        or config.stat().st_size > 65536
        or config.stat().st_nlink != 1
        or (path / "objects/info/alternates").exists()
    ):
        raise GitError("Local remote must be a bounded ordinary bare repository.")
    raw = run_git(
        path,
        ["config", "--file", str(config), "--no-includes", "--null", "--list"],
        limit=65536,
        git_directory=path,
    )
    bare = False
    for record in raw.split(b"\0"):
        if not record:
            continue
        key, _, value = record.partition(b"\n")
        name = key.decode("utf-8", errors="replace").lower()
        if name not in SAFE_KEYS and not SAFE_SECTION.fullmatch(name):
            raise GitError("Local remote configuration requires review.")
        if name == "core.bare":
            bare = value.lower() == b"true"
    if not bare:
        raise GitError("Local remotes must be bare repositories.")


@dataclass(frozen=True)
class FileChange:
    """Validated content-relative transition prepared before any mutation."""

    path: str
    revision: str | None
    document: Document | None


def tree_entries(root: Path, commit: str) -> dict[str, tuple[str, str]]:
    """Read Git tree descriptors with NUL path boundaries."""
    entries: dict[str, tuple[str, str]] = {}
    for row in run_git(root, ["ls-tree", "-r", "-z", commit]).split(b"\0"):
        if row:
            raw_descriptor, raw_path = row.split(b"\t", 1)
            mode, kind, oid = raw_descriptor.decode().split()
            entries[raw_path.decode("utf-8", errors="surrogateescape")] = (
                mode + " " + kind,
                oid,
            )
    return entries


class GitChanges:
    """Conservative local/remote operations; no shell, force-push, merge or rebase."""

    def __init__(self, content_root: str | Path) -> None:
        """Bind all operations to the operator's content scope."""
        self.repository = GitRepository(content_root)
        self.store = ContentStore(content_root)

    def _state(self, clean: bool = False) -> tuple[Path, RepositoryState]:
        """Refuse unfinished operations and optionally any user working changes."""
        state = self.repository.status()
        if state is None:
            raise GitError("No Git repository is configured.")
        if state.operation:
            raise ConflictError(
                "Repository needs attention; finish or recover its current operation first."
            )
        if clean and not state.clean:
            raise ConflictError(
                "Save and commit or preserve local changes before changing branches or updating."
            )
        return Path(state.root), state

    @serialized
    def create_branch(self, name: str) -> None:
        """Create a branch at HEAD without touching content or switching branches."""
        root, _ = self._state()
        run_git(root, ["branch", "--", branch_name(name)])

    @serialized
    def commit(self, paths: list[str], message: str) -> str:
        """Commit only selected content paths; leave unrelated staged content alone."""
        root, state = self._state()
        if state.detached:
            raise ConflictError("Choose a branch before committing.")
        if (
            not paths
            or len(paths) > 1000
            or not message.strip()
            or len(message.encode()) > 4096
            or "\0" in message
        ):
            raise GitError("Select content paths and provide a bounded commit message.")
        selected: list[str] = []
        for path in paths:
            validate_content_path(path)
            try:
                self.store.read(path)
            except FileNotFoundError:
                pass  # Explicitly selected deletions are normal Git changes.
            selected.append(
                (self.repository.content_root.relative_to(root) / path).as_posix()
            )
        run_git(root, ["add", "--", *selected])
        run_git(
            root,
            [
                "-c",
                "commit.gpgsign=false",
                "commit",
                "--only",
                "-m",
                message,
                "--",
                *selected,
            ],
        )
        return run_git(root, ["rev-parse", "HEAD"]).decode().strip()

    @serialized
    def fetch(self, remote: Remote) -> None:
        """Fetch only branch tracking refs from an explicit operator endpoint."""
        root, _ = self._state()
        protocol = remote.protocol()
        configuration = remote.configuration()
        try:
            run_git(
                root,
                [
                    "fetch",
                    "--no-tags",
                    "--no-recurse-submodules",
                    "--no-write-fetch-head",
                    "--",
                    remote.url,
                    f"refs/heads/*:refs/remotes/{remote.name}/*",
                ],
                timeout=30,
                protocol=protocol,
                configuration=configuration,
            )
        except GitError:
            raise RemoteError(UNREACHABLE) from None

    @serialized
    def push(self, remote: Remote, branch: str) -> None:
        """Push an explicit local branch without force; rejection is a normal conflict.

        On success the remote-tracking ref for the branch is moved to the pushed commit,
        as a named-remote push would, so ahead/behind counts need no fetch.
        """
        root, _ = self._state()
        name = branch_name(branch)
        protocol = remote.protocol()
        configuration = remote.configuration()
        try:
            output = run_git(
                root,
                [
                    "push",
                    "--porcelain",
                    "--no-follow-tags",
                    "--",
                    remote.url,
                    f"refs/heads/{name}:refs/heads/{name}",
                ],
                timeout=30,
                accepted=(0, 1),
                protocol=protocol,
                configuration=configuration,
            )
        except GitError:
            raise RemoteError(UNREACHABLE) from None
        # Porcelain prints one "<flag>\t<refspec>\t<summary>" line per reference:
        # "!" rejected, " " fast-forwarded, "*" created, "=" already up to date.
        flags = {
            line.split(b"\t", 1)[0] for line in output.splitlines() if b"\t" in line
        }
        if b"!" in flags:
            raise ConflictError(
                "The remote refused this push: it has commits this branch lacks, or it protects the branch. Nothing was overwritten; fetch and review before pushing again."
            )
        if not flags & {b" ", b"*", b"="}:
            raise RemoteError(UNREACHABLE)
        run_git(
            root,
            ["update-ref", f"refs/remotes/{remote.name}/{name}", f"refs/heads/{name}"],
        )

    @serialized
    def switch(self, branch: str) -> None:
        """Apply a branch's scoped Markdown through the recovering content store."""
        root, state = self._state(clean=True)
        name = branch_name(branch)
        target = (
            run_git(root, ["rev-parse", "--verify", f"refs/heads/{name}^{{commit}}"])
            .decode()
            .strip()
        )
        self._transition(root, target, f"refs/heads/{name}", state, update=False)

    @serialized
    def update(self, remote_name: str, branch: str) -> None:
        """Fast-forward to an already-fetched ref; never fetch, merge or rebase implicitly."""
        root, state = self._state(clean=True)
        if state.branch != branch_name(branch) or not REMOTE_NAME.fullmatch(
            remote_name
        ):
            raise ConflictError(
                "Update the current named branch using a configured remote name."
            )
        target = (
            run_git(
                root,
                [
                    "rev-parse",
                    "--verify",
                    f"refs/remotes/{remote_name}/{branch}^{{commit}}",
                ],
            )
            .decode()
            .strip()
        )
        try:
            run_git(root, ["merge-base", "--is-ancestor", "HEAD", target])
        except GitError:
            raise ConflictError(
                "Local and remote history cannot fast-forward. No content was changed."
            ) from None
        self._transition(root, target, f"refs/heads/{branch}", state, update=True)

    def _plan(self, root: Path, before: str, target: str) -> list[FileChange]:
        """Reject non-content changes and malformed blobs before modifying files."""
        paths = run_git(
            root,
            [
                "diff-tree",
                "--no-renames",
                "--no-commit-id",
                "--name-only",
                "-r",
                "-z",
                before,
                target,
            ],
        ).split(b"\0")
        tree = tree_entries(root, target)
        prior = tree_entries(root, before)
        plan: list[FileChange] = []
        total = 0
        scope = self.repository.content_root.relative_to(root)
        for raw in paths:
            if not raw:
                continue
            path = raw.decode("utf-8", errors="surrogateescape")
            try:
                relative = Path(path).relative_to(scope).as_posix()
            except ValueError:
                raise GitError(
                    "This transition changes files outside the content root; use external Git tooling."
                ) from None
            validate_content_path(relative)
            if not (self.store.root / relative).parent.is_dir():
                raise GitError(
                    "Create the required content directories before switching or updating."
                )
            try:
                snapshot = self.store.read(relative)
                revision = snapshot.revision
                if path not in prior or run_git(
                    root, ["cat-file", "blob", prior[path][1]], limit=MAX_BYTES
                ) != serialize(snapshot.document).encode("utf-8"):
                    raise ConflictError(
                        "Content changed while preparing the Git transition; no transition was started."
                    )
            except FileNotFoundError:
                revision = None
                if path in prior:
                    raise ConflictError(
                        "Content disappeared while preparing the Git transition."
                    ) from None
            document = None
            if path in tree:
                descriptor, oid = tree[path]
                if descriptor != "100644 blob":
                    raise GitError(
                        "Only regular, non-executable Markdown transitions are supported."
                    )
                try:
                    document = parse(
                        run_git(
                            root, ["cat-file", "blob", oid], limit=MAX_BYTES
                        ).decode("utf-8")
                    )
                except UnicodeDecodeError:
                    raise GitError("Remote Markdown must be UTF-8.") from None
                total += len(serialize(document).encode("utf-8"))
            elif not (self.store.root / ".archive").is_dir():
                raise GitError(
                    "Create .archive before a transition that removes content."
                )
            plan.append(FileChange(relative, revision, document))
            if len(plan) > 200 or total > 16 * 1024 * 1024:
                raise GitError(
                    "Transition exceeds 200 files or 16 MiB; use external Git tooling."
                )
        return plan

    def _transition(
        self,
        root: Path,
        target: str,
        reference: str,
        state: RepositoryState,
        update: bool,
    ) -> None:
        """Journal multi-file publication; leave a persistent recovery marker on failure."""
        before = run_git(root, ["rev-parse", "HEAD"]).decode().strip()
        plan = self._plan(root, before, target)
        marker = root / ".git/burzblog-transition.json"
        descriptor = os.open(
            marker, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600
        )
        with os.fdopen(descriptor, "w") as stream:
            json.dump(
                {
                    "before": before,
                    "target": target,
                    "reference": reference,
                    "branch": state.branch,
                    "paths": [change.path for change in plan],
                },
                stream,
            )
            stream.flush()
            os.fsync(stream.fileno())
        metadata = os.open(root / ".git", os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            os.fsync(metadata)
            for change in plan:
                if change.document is None:
                    if change.revision is not None:
                        self.store.archive(change.path, change.revision)
                elif change.revision is None:
                    self.store.create(change.path, change.document)
                else:
                    self.store.save(change.path, change.document, change.revision)
            run_git(
                root, ["read-tree", target]
            )  # Index only; never ask Git to overwrite the working tree.
            if update:
                run_git(root, ["update-ref", reference, target, before])
            else:
                run_git(root, ["symbolic-ref", "HEAD", reference])
            marker.unlink()
            os.fsync(metadata)
        except (OSError, ValueError):
            raise ConflictError(
                "Git transition needs recovery. Content versions and .git/burzblog-transition.json were retained; do not retry automatically."
            ) from None
        finally:
            os.close(metadata)
