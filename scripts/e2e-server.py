"""Start an isolated local fixture server for real browser acceptance tests."""

import os
import secrets
import subprocess
import sys
from pathlib import Path

import uvicorn

from burzblog.api import create_app
from burzblog.auth import Authentication
from burzblog.git_credentials import RemoteCredential
from burzblog.git_mutations import Remote
from burzblog.settings import Settings

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "tests"))
from support.git_https import basic, start  # test-only fixture module


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
    # A private HTTPS remote: real TLS, Basic authentication and git http-backend.
    token = "e2e-" + secrets.token_urlsafe(24)
    secret_directory = root / "secrets"
    secret_directory.mkdir(mode=0o700)
    token_file = secret_directory / "token"
    token_file.write_text(token + "\n")
    token_file.chmod(0o600)
    remote, _ = start(root / "https", basic("e2e", token), name="remote.git")
    settings = Settings(
        content,
        root / "state",
        "http://127.0.0.1:3000",
        Remote(
            "origin",
            remote.url,
            RemoteCredential("e2e", token_file),
            remote.certificate_authority,
        ),
        Path(__file__).resolve().parents[1] / "web/.output/public",
    )
    Authentication(settings.state_directory).initialize("browser fixture password")
    uvicorn.run(
        create_app(settings),
        host="127.0.0.1",
        port=3000,
        access_log=False,
        proxy_headers=False,
    )


if __name__ == "__main__":
    main()
