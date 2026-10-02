"""Provider-independent read-only Git state for ordinary local repositories."""

import os
import re
import stat
from dataclasses import dataclass
from pathlib import Path

from burzblog.git_process import GitError, run_git

SAFE_KEYS = frozenset(
    {
        "core.repositoryformatversion",
        "core.filemode",
        "core.bare",
        "core.logallrefupdates",
        "core.ignorecase",
        "core.precomposeunicode",
        "core.autocrlf",
        "core.eol",
        "core.safecrlf",
        "user.name",
        "user.email",
        "init.defaultbranch",
        "pull.rebase",
        "pull.ff",
        "push.default",
    }
)
SAFE_SECTION = re.compile(
    r"(?:remote\..+\.(?:url|pushurl|fetch)|branch\..+\.(?:remote|merge|rebase))\Z"
)


@dataclass(frozen=True)
class Change:
    """One repository-relative porcelain entry; paths are data, never commands."""

    path: str
    index: str
    worktree: str
    original_path: str | None = None

    @property
    def conflict(self) -> bool:
        """Identify Git's unmerged index combinations."""
        return self.index + self.worktree in {"DD", "AU", "UD", "UA", "DU", "AA", "UU"}


@dataclass(frozen=True)
class RepositoryState:
    """A fresh, bounded view of repository state; remotes expose names only."""

    root: str
    branch: str | None
    detached: bool
    changes: tuple[Change, ...]
    branches: tuple[str, ...]
    remotes: tuple[str, ...]
    upstream: str | None
    ahead: int | None
    behind: int | None
    operation: str | None
    changed_markdown: tuple[str, ...]

    @property
    def clean(self) -> bool:
        """Include untracked files and unfinished repository operations."""
        return not self.changes and self.operation is None


def parse_changes(output: bytes) -> tuple[Change, ...]:
    """Parse porcelain v1 -z, including spaces/newlines and rename source paths."""
    records = iter(output.decode("utf-8", errors="surrogateescape").split("\0"))
    changes: list[Change] = []
    for record in records:
        if not record:
            continue
        if len(record) < 4 or record[2] != " ":
            raise GitError("Git returned an unsupported status record.")
        original = None
        if "R" in record[:2] or "C" in record[:2]:
            original = next(records, None)
            if not original:
                raise GitError("Git returned an incomplete rename record.")
        changes.append(Change(record[3:], record[0], record[1], original))
    return tuple(changes)


class GitRepository:
    """Inspect an operator-selected working tree without executing its configuration."""

    def __init__(self, content_root: str | Path) -> None:
        """Keep configured content scope separate from the repository root."""
        self.content_root = Path(os.path.abspath(content_root))

    def discover(self) -> Path | None:
        """Find a normal .git directory; refuse symlinks, linked worktrees and bare repos."""
        if (
            not self.content_root.is_dir()
            or self.content_root.resolve() != self.content_root
        ):
            raise GitError(
                "Git content root must be an existing directory without symlinks."
            )
        for parent in (self.content_root, *self.content_root.parents):
            marker = parent / ".git"
            if not marker.exists() and not marker.is_symlink():
                continue
            info = marker.lstat()
            if (
                not stat.S_ISDIR(info.st_mode)
                or info.st_uid != os.geteuid()
                or info.st_mode & 0o022
            ):
                raise GitError(
                    "Only privately owned ordinary Git working trees are supported."
                )
            if not (marker / "config").exists() and not (marker / "HEAD").exists():
                continue
            return parent
        return None

    def checked_root(self) -> Path:
        """Reject executable/indirect local configuration before any status inspection."""
        root = self.discover()
        if root is None:
            raise GitError("No Git repository is configured.")
        metadata = root / ".git"
        count = 0
        for directory, directories, files in os.walk(metadata, followlinks=False):
            for entry_name in (*directories, *files):
                count += 1
                entry_info = (Path(directory) / entry_name).lstat()
                if (
                    count > 100000
                    or stat.S_ISLNK(entry_info.st_mode)
                    or entry_info.st_uid != os.geteuid()
                    or entry_info.st_mode & 0o022
                ):
                    raise GitError(
                        "Repository metadata must be privately owned, bounded and free of symlinks."
                    )
        config = metadata / "config"
        info = config.lstat()
        if (
            not stat.S_ISREG(info.st_mode)
            or info.st_nlink != 1
            or info.st_uid != os.geteuid()
            or info.st_mode & 0o022
            or info.st_size > 65536
        ):
            raise GitError(
                "Repository configuration is not a supported private regular file."
            )
        for forbidden in (
            "config.worktree",
            "commondir",
            "objects/info/alternates",
            "shallow",
        ):
            if os.path.lexists(metadata / forbidden):
                raise GitError("Repository extensions require external Git tooling.")
        raw = run_git(
            root,
            ["config", "--file", str(config), "--no-includes", "--null", "--list"],
            limit=65536,
        )
        for entry in raw.split(b"\0"):
            if not entry:
                continue
            key, _, value = entry.partition(b"\n")
            name = key.decode("utf-8", errors="replace").lower()
            if name not in SAFE_KEYS and not SAFE_SECTION.fullmatch(name):
                raise GitError(
                    "Repository configuration requires review; includes, helpers and custom commands are unsupported."
                )
            if (name == "core.bare" and value.lower() != b"false") or (
                name == "core.repositoryformatversion" and value != b"0"
            ):
                raise GitError(
                    "Only ordinary non-bare repositories are currently supported."
                )
        return root

    def status(self) -> RepositoryState | None:
        """Read state without network access, index refresh writes or URL disclosure."""
        if self.discover() is None:
            return None
        root = self.checked_root()
        changes = parse_changes(
            run_git(
                root,
                [
                    "status",
                    "--porcelain=v1",
                    "-z",
                    "--untracked-files=all",
                    "--ignore-submodules=all",
                ],
            )
        )
        scope = self.content_root.relative_to(root)
        changes = tuple(
            change
            for change in changes
            if not (
                change.index == "?"
                and any(
                    Path(change.path).is_relative_to(scope / hidden)
                    for hidden in (".burzblog-recovery", ".archive")
                )
            )
        )
        branch = (
            run_git(
                root, ["symbolic-ref", "--quiet", "--short", "HEAD"], accepted=(0, 1)
            )
            .decode()
            .strip()
            or None
        )
        branch_rows = run_git(
            root,
            [
                "for-each-ref",
                "--format=%(refname:short)%00%(upstream:short)%00%(upstream:track)",
                "refs/heads/",
            ],
        ).decode("utf-8", errors="surrogateescape")
        branches: list[str] = []
        upstream = None
        ahead = behind = None
        for row in branch_rows.splitlines():
            name, tracking, counts = row.split("\0")
            branches.append(name)
            if name == branch and tracking:
                upstream = tracking
                if counts != "[gone]":
                    ahead_match = re.search(r"ahead (\d+)", counts)
                    behind_match = re.search(r"behind (\d+)", counts)
                    ahead = int(ahead_match[1]) if ahead_match else 0
                    behind = int(behind_match[1]) if behind_match else 0
        remotes = tuple(
            run_git(root, ["remote"])
            .decode("utf-8", errors="surrogateescape")
            .splitlines()
        )
        operation = None
        for marker, label in (
            ("burzblog-transition.json", "recovery"),
            ("MERGE_HEAD", "merge"),
            ("rebase-merge", "rebase"),
            ("rebase-apply", "rebase"),
            ("CHERRY_PICK_HEAD", "cherry-pick"),
            ("REVERT_HEAD", "revert"),
        ):
            if os.path.lexists(root / ".git" / marker):
                operation = label
                break
        if any(change.conflict for change in changes):
            operation = operation or "conflict"
        scope = self.content_root.relative_to(root)
        markdown = tuple(
            change.path
            for change in changes
            if change.path.endswith(".md")
            and Path(change.path).is_relative_to(scope)
            and not any(part.startswith(".") for part in Path(change.path).parts)
        )
        return RepositoryState(
            str(root),
            branch,
            branch is None,
            changes,
            tuple(branches),
            remotes,
            upstream,
            ahead,
            behind,
            operation,
            markdown,
        )
