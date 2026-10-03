"""Operator-only setup/reset and loopback server command, never a web setup wizard."""

import argparse
import getpass
import sqlite3
import sys

import uvicorn

from burzblog.api import create_app
from burzblog.auth import Authentication
from burzblog.errors import ContentError
from burzblog.settings import Settings


def main() -> int:
    """Initialize credentials through a terminal, or serve the configured API."""
    parser = argparse.ArgumentParser(description="BurzBlog authenticated API")
    parser.add_argument("command", choices=["init-admin", "reset-admin", "serve"])
    parser.add_argument("--port", type=int, default=8000)
    parser.add_argument(
        "--host",
        default="127.0.0.1",
        help="Bind address. Keep the loopback default; use 0.0.0.0 only inside a container whose published port is itself private or proxied.",
    )
    arguments = parser.parse_args()
    try:
        if arguments.command in {"init-admin", "reset-admin"}:
            # Terminal recovery needs safe authentication state only, never the
            # Git remote, its secrets, network access or serving configuration.
            settings = Settings.for_administration()
            settings.validate_storage()
            authentication = Authentication(
                settings.state_directory, create=arguments.command == "init-admin"
            )
            password = getpass.getpass("Administrator password: ")
            if password != getpass.getpass("Confirm password: "):
                raise ContentError("Passwords did not match.")
            if arguments.command == "init-admin":
                authentication.initialize(password)
                print("Administrator initialized. Keep the state directory private.")
            else:
                authentication.reset(password)
                print("Administrator password replaced; all sessions were signed out.")
        else:
            settings = Settings.from_environment()
            settings.validate()
            uvicorn.run(
                create_app(settings),
                host=arguments.host,
                port=arguments.port,
                proxy_headers=False,
                access_log=False,
                limit_concurrency=32,
            )
    except ContentError as error:
        # Messages are content- and secret-free by contract; show the specific cause.
        print(f"Setup failed: {error}", file=sys.stderr)
        return 1
    except sqlite3.Error:
        print(
            "Setup failed: Authentication state is unreadable or not a BurzBlog database; restore the state directory from backup.",
            file=sys.stderr,
        )
        return 1
    except OSError as error:
        print(
            f"Setup failed: {error.strerror or 'filesystem error'}. Check configured paths and private directory permissions.",
            file=sys.stderr,
        )
        return 1
    return 0
