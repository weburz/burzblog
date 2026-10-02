"""Local-only demonstration interface; no server or implicit network access."""

import argparse
import json
import sys
from dataclasses import asdict

from burzblog.document import MAX_BYTES, Document, parse, serialize
from burzblog.errors import ContentError
from burzblog.git_domain import GitRepository
from burzblog.git_mutations import GitChanges, Remote
from burzblog.storage import ContentStore


def _stdin() -> str:
    """Read bounded UTF-8 input without platform newline translation."""
    data = sys.stdin.buffer.read(MAX_BYTES + 1)
    if len(data) > MAX_BYTES:
        raise ContentError("Input exceeds 2 MiB.")
    try:
        return data.decode("utf-8")
    except UnicodeDecodeError:
        raise ContentError("Input must be UTF-8.") from None


def main() -> int:
    """Execute explicit local operations and return a process exit status."""
    parser = argparse.ArgumentParser(
        description="BurzBlog Markdown foundation (local CLI)"
    )
    parser.add_argument(
        "--root", required=True, help="Existing privately owned content directory"
    )
    commands = parser.add_subparsers(dest="command", required=True)
    commands.add_parser("list", help="Discover content paths")
    commands.add_parser(
        "git-status", help="Inspect Git without changing files or contacting remotes"
    )
    for command in ("git-create-branch", "git-switch", "git-update", "git-push"):
        git_command = commands.add_parser(command)
        git_command.add_argument("branch")
        if command in ("git-update", "git-push"):
            git_command.add_argument("--remote", default="origin")
        if command == "git-push":
            git_command.add_argument(
                "--url",
                required=True,
                help="Operator-approved remote endpoint; no embedded credentials",
            )
    fetch = commands.add_parser("git-fetch")
    fetch.add_argument("--remote", default="origin")
    fetch.add_argument("--url", required=True)
    commit = commands.add_parser("git-commit")
    commit.add_argument("paths", nargs="+")
    commit.add_argument("--message", required=True)
    for command in ("read", "create", "save", "rename", "archive"):
        sub = commands.add_parser(command)
        sub.add_argument("path")
        if command == "create":
            sub.add_argument("--title", required=True)
            sub.add_argument("--slug", required=True)
        if command in ("save", "rename", "archive"):
            sub.add_argument("--revision", required=True)
        if command == "rename":
            sub.add_argument("destination")
    args = parser.parse_args()
    store = ContentStore(args.root)
    git_changes = GitChanges(args.root)
    try:
        if args.command == "list":
            print(json.dumps(store.discover()))
        elif args.command == "git-status":
            state = GitRepository(args.root).status()
            print(
                json.dumps(
                    {
                        "repository": asdict(state) | {"clean": state.clean}
                        if state
                        else None
                    }
                )
            )
        elif args.command == "read":
            snapshot = store.read(args.path)
            print(
                json.dumps(
                    {
                        "revision": snapshot.revision,
                        "markdown": serialize(snapshot.document),
                    }
                )
            )
        elif args.command == "git-create-branch":
            git_changes.create_branch(args.branch)
        elif args.command == "git-switch":
            git_changes.switch(args.branch)
        elif args.command == "git-commit":
            print(git_changes.commit(args.paths, args.message))
        elif args.command == "git-fetch":
            git_changes.fetch(Remote(args.remote, args.url))
        elif args.command == "git-update":
            git_changes.update(args.remote, args.branch)
        elif args.command == "git-push":
            git_changes.push(Remote(args.remote, args.url), args.branch)
        elif args.command == "create":
            print(
                store.create(args.path, Document.new(args.title, args.slug, _stdin()))
            )
        elif args.command == "save":
            print(store.save(args.path, parse(_stdin()), args.revision))
        elif args.command == "rename":
            store.rename(args.path, args.destination, args.revision)
        elif args.command == "archive":
            print(store.archive(args.path, args.revision))
    except ContentError as error:
        print(str(error), file=sys.stderr)
        return 1
    except OSError:
        print(
            "Filesystem operation failed. Check paths, ownership and permissions; inspect disk before retrying.",
            file=sys.stderr,
        )
        return 1
    return 0
