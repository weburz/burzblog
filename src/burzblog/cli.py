"""Local-only demonstration interface; no server or implicit network access."""

import argparse
import json
import sys

from burzblog.document import MAX_BYTES, Document, parse, serialize
from burzblog.errors import ContentError
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
    try:
        if args.command == "list":
            print(json.dumps(store.discover()))
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
