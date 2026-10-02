"""Start an isolated local fixture server for real browser acceptance tests."""

import os
import subprocess
from pathlib import Path

import uvicorn

from burzblog.api import create_app
from burzblog.auth import Authentication
from burzblog.git_mutations import Remote
from burzblog.settings import Settings


def git(root: Path, *arguments: str) -> None:
    """Execute fixed fixture setup arguments without a shell."""
    subprocess.run(  # noqa: S603 - controlled fixture setup, never user input
        ["git", "-C", str(root), *arguments],  # noqa: S607 - developer Git
        check=True,
        capture_output=True,
    )


def main() -> None:
    """Keep content and credentials in a fresh test-owned temporary directory."""
    root = Path(os.environ["BURZBLOG_E2E_ROOT"])
    repository = root / "repo"
    repository.mkdir()
    content = repository / "content"
    (content / "posts").mkdir(parents=True)
    (content / ".archive").mkdir()
    git(repository, "init", "--initial-branch=main")
    git(repository, "config", "user.name", "Browser fixture")
    git(repository, "config", "user.email", "fixture@example.invalid")
    git(
        repository,
        "-c",
        "commit.gpgsign=false",
        "commit",
        "--allow-empty",
        "-m",
        "fixture",
    )
    remote = root / "remote.git"
    git(root, "init", "--bare", "--initial-branch=main", str(remote))
    settings = Settings(
        content, root / "state", "http://127.0.0.1:3000", Remote("origin", str(remote))
    )
    Authentication(settings.state_directory).initialize("browser fixture password")
    uvicorn.run(
        create_app(settings),
        host="127.0.0.1",
        port=8000,
        access_log=False,
        proxy_headers=False,
    )


if __name__ == "__main__":
    main()
