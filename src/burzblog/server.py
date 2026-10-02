"""Operator-only setup and loopback server command, never a web setup wizard."""

import argparse
import getpass
import sys

import uvicorn

from burzblog.api import create_app
from burzblog.auth import Authentication
from burzblog.errors import ContentError
from burzblog.settings import Settings


def main() -> int:
    """Initialize credentials through a terminal, or serve the configured API."""
    parser = argparse.ArgumentParser(description="BurzBlog authenticated API")
    parser.add_argument("command", choices=["init-admin", "serve"])
    parser.add_argument("--port", type=int, default=8000)
    arguments = parser.parse_args()
    try:
        settings = Settings.from_environment()
        settings.validate()
        if arguments.command == "init-admin":
            password = getpass.getpass("Administrator password: ")
            if password != getpass.getpass("Confirm password: "):
                raise ContentError("Passwords did not match.")
            Authentication(settings.state_directory).initialize(password)
            print("Administrator initialized. Keep the state directory private.")
        else:
            uvicorn.run(
                create_app(settings),
                host="127.0.0.1",
                port=arguments.port,
                proxy_headers=False,
                access_log=False,
                limit_concurrency=32,
            )
    except (ContentError, OSError):
        print(
            "Setup failed. Check configuration, private directory permissions and administrator initialization.",
            file=sys.stderr,
        )
        return 1
    return 0
