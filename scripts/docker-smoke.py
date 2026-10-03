"""Container acceptance test for the BurzBlog browser application.

Runs the image through compose.dist.yaml against disposable host directories and
a disposable authenticated HTTPS Git server on the Compose network. Requires a
Docker daemon; never touches operator content. Build the image first
(`task docker:build`). The fixture directory is kept for inspection.
"""

import http.cookiejar
import json
import os
import re
import secrets
import shutil
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
from pathlib import Path

PROJECT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(PROJECT / "tests"))
from support.git_https import certificate  # test-only fixture module

IMAGE = os.environ.get("BURZBLOG_TEST_IMAGE", "burzblog:local")
PASSWORD = "container smoke password"  # noqa: S105 - disposable fixture
USERNAME = "smoke"
checks: list[str] = []


def run(*command: str | Path, stdin: str | None = None, check: bool = True) -> str:
    """Run a fixed command without a shell; return stdout."""
    completed = subprocess.run(  # noqa: S603 - fixed developer commands
        [str(part) for part in command],
        input=stdin,
        capture_output=True,
        text=True,
        check=False,
    )
    if check and completed.returncode != 0:
        raise RuntimeError(
            f"{command[:4]} failed ({completed.returncode}): {completed.stderr[-2000:]}"
        )
    return completed.stdout


def expect(condition: object, detail: object = "") -> None:
    """Fail loudly; unlike assert, never stripped by python -O."""
    if not condition:
        raise RuntimeError(f"Acceptance check failed {detail}".strip())


def passed(description: str) -> None:
    """Record and print one verified acceptance criterion."""
    checks.append(description)
    print(f"ok - {description}", flush=True)


def git(directory: Path, *arguments: str) -> str:
    """Host-side Git for fixture setup and verification."""
    return run("git", "-C", directory, *arguments)


def free_port() -> int:
    """Pick an unused host loopback port for this run."""
    with socket.socket() as probe:
        probe.bind(("127.0.0.1", 0))
        return int(probe.getsockname()[1])


class Browser:
    """Minimal same-origin client: cookies, Origin and CSRF like the real UI."""

    def __init__(self, origin: str) -> None:
        """Start without a session."""
        self.origin = origin
        self.jar = http.cookiejar.CookieJar()
        self.opener = urllib.request.build_opener(
            urllib.request.HTTPCookieProcessor(self.jar)
        )
        self.csrf = ""
        self.bodies: list[str] = []

    def request(
        self, method: str, path: str, body: object | None = None
    ) -> tuple[int, dict[str, str], str]:
        """Send one request and keep the response body for later secret scans."""
        headers = {"Origin": self.origin}
        data = None
        if body is not None:
            data = json.dumps(body).encode()
            headers["Content-Type"] = "application/json"
        if self.csrf:
            headers["X-CSRF-Token"] = self.csrf
        request = urllib.request.Request(  # noqa: S310 - fixed loopback URL
            self.origin + path, data=data, method=method, headers=headers
        )
        try:
            with self.opener.open(request, timeout=30) as response:
                status, raw, received = (
                    response.status,
                    response.read(),
                    response.headers,
                )
        except urllib.error.HTTPError as error:
            status, raw, received = error.code, error.read(), error.headers
        text = raw.decode("utf-8", errors="replace")
        self.bodies.append(text)
        return status, {key.lower(): value for key, value in received.items()}, text

    def json(self, method: str, path: str, body: object | None = None) -> dict:
        """Require success and decode JSON."""
        status, _, text = self.request(method, path, body)
        if status >= 300:
            raise RuntimeError(f"{method} {path} returned {status}: {text}")
        return json.loads(text)

    def login(self) -> None:
        """Authenticate as the administrator initialized out of band."""
        self.csrf = self.json("POST", "/api/login", {"password": PASSWORD})["csrf"]


def wait_healthy(container: str) -> None:
    """Wait for the image HEALTHCHECK, which probes the unauthenticated session route."""
    for _ in range(90):
        status = run(
            "docker", "inspect", "--format", "{{.State.Health.Status}}", container
        ).strip()
        if status == "healthy":
            return
        time.sleep(1)
    raise RuntimeError(f"Container did not become healthy: {status}")


def prepare(fixture: Path, token: str, port: int) -> tuple[Path, list[str]]:
    """Create site/state/secrets directories and the Compose environment."""
    site = fixture / "site"
    content = site / "content"
    (content / "posts").mkdir(parents=True)
    (content / ".archive").mkdir()
    (content / "posts/hello.md").write_text("---\ntitle: Hello\n---\nFirst post.\n")
    git(site, "init", "--quiet", "--initial-branch=main")
    git(site, "config", "user.name", "Smoke")
    git(site, "config", "user.email", "smoke@example.invalid")
    git(site, "remote", "add", "origin", "https://gitfixture:8443/site.git")
    git(site, "add", "content/posts/hello.md")
    git(site, "-c", "commit.gpgsign=false", "commit", "--quiet", "-m", "initial")
    for private in ("state", "secrets", "git"):
        (fixture / private).mkdir(mode=0o700)
    authority, _ = certificate(fixture / "git", "fixture", "gitfixture")
    shutil.copy(authority, fixture / "secrets/fixture-ca.pem")
    (fixture / "secrets/fixture-ca.pem").chmod(0o644)
    for path in (fixture / "secrets/git-token", fixture / "git/expected-token"):
        path.write_text(token + "\n")
        path.chmod(0o600)
    environment = fixture / "smoke.env"
    environment.write_text(
        "\n".join(
            [
                f"BURZBLOG_IMAGE={IMAGE}",
                f"BURZBLOG_UID={os.getuid()}",
                f"BURZBLOG_GID={os.getgid()}",
                f"BURZBLOG_ORIGIN=http://127.0.0.1:{port}",
                f"BURZBLOG_PORT={port}",
                f"BURZBLOG_SITE_DIR={site}",
                "BURZBLOG_CONTENT_SUBDIR=content",
                f"BURZBLOG_STATE_PATH={fixture / 'state'}",
                f"BURZBLOG_SECRETS_DIR={fixture / 'secrets'}",
                "BURZBLOG_REMOTE_URL=https://gitfixture:8443/site.git",
                f"BURZBLOG_REMOTE_USERNAME={USERNAME}",
                "BURZBLOG_REMOTE_TOKEN_FILE=/secrets/git-token",
                "BURZBLOG_REMOTE_CA_FILE=/secrets/fixture-ca.pem",
                "",
            ]
        )
    )
    return site, [
        "docker",
        "compose",
        "--project-name",
        fixture.name.lower(),
        "--file",
        str(PROJECT / "compose.dist.yaml"),
        "--env-file",
        str(environment),
    ]


def wait_for_log(compose: list[str], text: str) -> None:
    """Wait until the service logs contain an expected operator message."""
    for _ in range(60):
        if text in run(*compose, "logs", "--no-color", check=False):
            return
        time.sleep(1)
    raise RuntimeError(f"Expected log message not seen: {text}")


def archive(source: Path, name: str, target: Path) -> None:
    """Back up one directory with the tar options docs/backup.md uses."""
    run(
        "tar",
        "--numeric-owner",
        "--create",
        "--preserve-permissions",
        "--gzip",
        "--file",
        target / f"{name}.tar.gz",
        "--directory",
        source,
        name,
    )


def extract(archive_file: Path, destination: Path) -> None:
    """Restore one archive with its recorded permissions."""
    run(
        "tar",
        "--extract",
        "--preserve-permissions",
        "--gzip",
        "--file",
        archive_file,
        "--directory",
        destination,
    )


def restore_drill(
    fixture: Path,
    site: Path,
    compose: list[str],
    container: str,
    origin: str,
    password: str,
    token: str,
) -> None:
    """Back up, destroy, restore (secret last) and verify the deployment."""
    before = Browser(origin)
    before.csrf = before.json("POST", "/api/login", {"password": password})["csrf"]
    article = before.json("GET", "/api/post?path=posts/hello.md")
    history = git(site, "log", "--format=%H %s")
    remotes = git(site, "remote", "--verbose")
    tracking = git(site, "for-each-ref", "refs/remotes")
    run(*compose, "stop")
    backups = fixture / "backups"
    backups.mkdir(mode=0o700)
    for name in ("site", "state"):
        archive(fixture, name, backups)
    private = backups / "secrets"  # Kept apart: a real operator stores this elsewhere.
    private.mkdir(mode=0o700)
    archive(fixture, "secrets", private)
    shutil.copy(fixture / "smoke.env", backups / "deployment.env")
    passed("stopped backup of site (with .git), state, separate secrets and config")

    run(*compose, "down")
    for name in ("site", "state", "secrets"):
        shutil.rmtree(fixture / name)
    for name in ("site", "state"):
        extract(backups / f"{name}.tar.gz", fixture)
    (fixture / "secrets").mkdir(mode=0o700)  # Empty until the secret is restored.
    run(*compose, "up", "--detach", "--no-build")
    wait_for_log(compose, "file is missing or unreadable by the service user")
    run(*compose, "down")
    passed("restore without the Git secret fails closed with a specific message")
    # Recovery must not depend on the Git secret that `serve` still requires.
    run(
        *compose,
        "run",
        "--rm",
        "-T",
        "burzblog",
        "reset-admin",
        stdin=f"{password}\n{password}\n",
    )
    passed("reset-admin works while the Git secret is missing and serve refuses")

    shutil.rmtree(fixture / "secrets")
    extract(private / "secrets.tar.gz", fixture)
    expect((fixture / "secrets/git-token").stat().st_mode & 0o777 == 0o600)
    expect((fixture / "state").stat().st_mode & 0o777 == 0o700)
    run(*compose, "up", "--detach", "--no-build")
    wait_healthy(container)
    after = Browser(origin)
    after.csrf = after.json("POST", "/api/login", {"password": password})["csrf"]
    restored = after.json("GET", "/api/post?path=posts/hello.md")
    expect(restored == article, "content and revision identical after restore")
    expect(git(site, "log", "--format=%H %s") == history, "Git history")
    expect(git(site, "remote", "--verbose") == remotes, "Git remotes")
    expect(git(site, "for-each-ref", "refs/remotes") == tracking, "tracking refs")
    git(site, "fsck", "--strict", "--no-progress")
    saved = after.json(
        "PUT",
        "/api/post",
        {
            "path": article["path"],
            "revision": article["revision"],
            "markdown": restored["markdown"] + "After restore.\n",
        },
    )
    status, _, _ = after.request(
        "PUT",
        "/api/post",
        {
            "path": article["path"],
            "revision": article["revision"],
            "markdown": "stale\n",
        },
    )
    expect(status == 409 and saved["revision"] != article["revision"], "revisions")
    after.json(
        "POST",
        "/api/git",
        {"action": "commit", "paths": [article["path"]], "message": "After restore"},
    )
    after.json("POST", "/api/git", {"action": "push", "branch": "main"})
    after.json("POST", "/api/git", {"action": "fetch"})
    served = fixture / "git/served/site.git"
    expect("After restore." in git(served, "show", "main:content/posts/hello.md"))
    expect(token not in "\n".join(after.bodies), "token in API bodies")
    passed(
        "restored deployment: login, content, revisions, Git history/remotes and "
        "private push/fetch all intact"
    )


def main() -> None:
    """Verify the deployment contract end to end."""
    fixture = Path(tempfile.mkdtemp(prefix="burzblog-container-"))
    token = "smoke-" + secrets.token_urlsafe(24)
    port = free_port()
    origin = f"http://127.0.0.1:{port}"
    site, compose = prepare(fixture, token, port)
    project = fixture.name.lower()
    container = f"{project}-burzblog-1"
    git_container = f"{project}-git"
    try:
        run(*compose, "config", "--quiet")
        passed("compose.dist.yaml renders with the fixture environment")

        run(
            *compose,
            "run",
            "--rm",
            "-T",
            "burzblog",
            "init-admin",
            stdin=f"{PASSWORD}\n{PASSWORD}\n",
        )
        database = fixture / "state/authentication.sqlite3"
        expect(database.stat().st_uid == os.getuid())
        expect((fixture / "state").stat().st_mode & 0o777 == 0o700)
        passed("init-admin wrote private SQLite state as the host UID")

        run(*compose, "up", "--detach", "--no-build")
        run(
            "docker",
            "run",
            "--detach",
            "--name",
            git_container,
            "--network",
            f"{project}_default",
            "--network-alias",
            "gitfixture",
            "--user",
            f"{os.getuid()}:{os.getgid()}",
            "--read-only",
            "--tmpfs",
            "/tmp",  # noqa: S108 - container-private tmpfs
            "--mount",
            f"type=bind,source={fixture / 'git'},target=/git",
            "--mount",
            f"type=bind,source={PROJECT / 'tests/support'},target=/fixture,readonly",
            "--entrypoint",
            "python",
            IMAGE,
            "/fixture/git_https.py",
            "--directory",
            "/git",
            "--hostname",
            "gitfixture",
            "--username",
            USERNAME,
            "--token-file",
            "/git/expected-token",
            "--certificate",
            "/git/fixture-ca.pem",
            "--key",
            "/git/fixture-key.pem",
        )
        wait_healthy(container)
        passed("production service started and passed its HEALTHCHECK")

        browser = Browser(origin)
        status, headers, shell = browser.request("GET", "/")
        expect(status == 200 and '<div id="__nuxt"' in shell)
        expect("script-src 'self' 'sha256-" in headers["content-security-policy"])
        asset = re.search(r'src="(/_nuxt/[^"]+\.js)"', shell)
        expect(asset is not None)
        status, headers, _ = browser.request("GET", asset[1])
        expect(status == 200 and "immutable" in headers["cache-control"])
        passed("browser shell and fingerprinted assets served same-origin with CSP")
        for probe in ("/secrets/git-token", "/_nuxt/../../secrets/git-token", "/state"):
            expect(browser.request("GET", probe)[0] == 404)
        expect(browser.request("GET", "/api/posts")[0] == 401)
        passed("secrets/state are not reachable over HTTP; API requires login")

        browser.login()
        listed = browser.json("GET", "/api/posts")
        expect([item["path"] for item in listed["items"]] == ["posts/hello.md"])
        article = browser.json("GET", "/api/post?path=posts/hello.md")
        saved = browser.json(
            "PUT",
            "/api/post",
            {
                "path": article["path"],
                "revision": article["revision"],
                "markdown": "---\ntitle: Hello\n---\nEdited in the container.\n",
            },
        )
        on_disk = site / "content/posts/hello.md"
        expect("Edited in the container." in on_disk.read_text())
        expect(
            (on_disk.stat().st_uid, on_disk.stat().st_gid)
            == (
                os.getuid(),
                os.getgid(),
            )
        )
        created = browser.json(
            "POST", "/api/posts", {"title": "New", "slug": "new", "body": "Body\n"}
        )
        new_file = site / "content" / created["path"]
        expect(new_file.stat().st_uid == os.getuid())
        passed("login, list, read, save and create work on the bind-mounted site")
        passed("files written by the container keep the host UID/GID")

        with on_disk.open("a") as stream:
            stream.write("External host edit.\n")
        fresh = browser.json("GET", "/api/post?path=posts/hello.md")
        expect("External host edit." in fresh["markdown"])
        expect(fresh["revision"] != saved["revision"])
        status, _, _ = browser.request(
            "PUT",
            "/api/post",
            {
                "path": saved["path"],
                "revision": saved["revision"],
                "markdown": "stale overwrite\n",
            },
        )
        expect(status == 409 and "External host edit." in on_disk.read_text())
        passed("external host edits are visible and stale saves are refused")

        state = browser.json("GET", "/api/git")
        expect(
            state["remote"]
            == {
                "name": "origin",
                "transport": "https",
                "authenticated": True,
            }
        )
        browser.json(
            "POST",
            "/api/git",
            {
                "action": "commit",
                "paths": ["posts/hello.md", created["path"]],
                "message": "Container commit",
            },
        )
        browser.json("POST", "/api/git", {"action": "push", "branch": "main"})
        served = fixture / "git/served/site.git"
        expect(
            "External host edit." in git(served, "show", "main:content/posts/hello.md")
        )
        browser.json("POST", "/api/git", {"action": "fetch"})
        passed("authenticated HTTPS push and fetch to a private Git fixture")
        (fixture / "secrets/git-token").write_text("revoked\n")
        status, _, body = browser.request("POST", "/api/git", {"action": "fetch"})
        expect(status == 502 and json.loads(body)["error"]["code"] == "remote")
        (fixture / "secrets/git-token").write_text(token + "\n")
        browser.json("POST", "/api/git", {"action": "fetch"})
        passed("revoked token reports a remote error; rotation needs no restart")

        uid = run("docker", "exec", container, "id", "-u").strip()
        pid1 = run("docker", "exec", container, "cat", "/proc/1/status")
        expect(uid == str(os.getuid()) and f"Uid:\t{os.getuid()}" in pid1)
        passed(f"service runs as non-root UID {uid}")
        for path, writable in (
            ("/app/probe", False),
            ("/usr/probe", False),
            ("/secrets/probe", False),
            ("/tmp/probe", True),  # noqa: S108 - asserting the tmpfs
        ):
            result = subprocess.run(  # noqa: S603 - fixed probe
                ["docker", "exec", container, "touch", path],  # noqa: S607
                capture_output=True,
                check=False,
            )
            expect((result.returncode == 0) is writable, path)
        passed("root filesystem and secrets mount are read-only; /tmp is tmpfs")
        inspected = json.loads(run("docker", "inspect", container))[0]
        expect(inspected["HostConfig"]["CapDrop"] == ["ALL"])
        expect("no-new-privileges:true" in inspected["HostConfig"]["SecurityOpt"])
        expect(inspected["HostConfig"]["RestartPolicy"]["Name"] == "unless-stopped")
        expect(inspected["Config"]["StopTimeout"] == 45, "stop grace period")
        bindings = inspected["HostConfig"]["PortBindings"]["8080/tcp"]
        expect([binding["HostIp"] for binding in bindings] == ["127.0.0.1"])
        passed(
            "capabilities dropped, no-new-privileges, loopback-only port, restart policy"
        )

        logs = run(*compose, "logs", "--no-color")
        exposed = [
            label
            for label, text in (
                ("docker inspect", json.dumps(inspected)),
                ("container logs", logs),
                ("API responses", "\n".join(browser.bodies)),
                (
                    "site .git",
                    "".join(
                        path.read_text(errors="replace")
                        for path in (site / ".git").rglob("*")
                        if path.is_file() and path.stat().st_size < 1_000_000
                    ),
                ),
                ("state", database.read_bytes().decode("latin-1")),
            )
            if token in text
        ]
        expect(exposed == [], exposed)
        passed("token absent from inspect/env, logs, API bodies, .git and state")

        cookies = list(browser.jar)
        run("docker", "restart", container)
        wait_healthy(container)
        expect(browser.json("GET", "/api/posts")["total"] == 2)
        passed("restart keeps sessions, content and configuration")

        run(*compose, "down")
        run(*compose, "up", "--detach", "--no-build")
        wait_healthy(container)
        expect({cookie.name for cookie in browser.jar} == {c.name for c in cookies})
        expect(browser.json("GET", "/api/posts")["total"] == 2)
        replacement = Browser(origin)
        replacement.login()
        expect("External host edit." in on_disk.read_text())
        passed("container replacement preserves auth database, sessions and content")

        replaced = PASSWORD[::-1]
        run(
            *compose,
            "run",
            "--rm",
            "-T",
            "burzblog",
            "reset-admin",
            stdin=f"{replaced}\n{replaced}\n",
        )
        expect(browser.request("GET", "/api/posts")[0] == 401, "old session revoked")
        status, _, _ = Browser(origin).request(
            "POST", "/api/login", {"password": PASSWORD}
        )
        expect(status == 401, "old password refused")
        renewed = Browser(origin)
        renewed.csrf = renewed.json("POST", "/api/login", {"password": replaced})[
            "csrf"
        ]
        expect(renewed.json("GET", "/api/posts")["total"] == 2)
        expect(oct(database.stat().st_mode & 0o777) == "0o600", "state mode kept")
        passed("reset-admin replaces the password and revokes sessions")

        listing = run(
            *compose,
            "run",
            "--rm",
            "-T",
            "--entrypoint",
            "burzblog",
            "burzblog",
            "--root",
            "/site/content",
            "list",
        )
        expect("posts/hello.md" in listing)
        passed("CLI remains usable through the same image")

        restore_drill(fixture, site, compose, container, origin, replaced, token)

        run(*compose, "stop")
        exit_code = json.loads(run("docker", "inspect", container))[0]["State"][
            "ExitCode"
        ]
        expect(exit_code in (0, 143), exit_code)
        passed(f"graceful stop (exit code {exit_code})")

        size = int(run("docker", "image", "inspect", "--format", "{{.Size}}", IMAGE))
        tools = run(
            "docker",
            "run",
            "--rm",
            "--entrypoint",
            "sh",
            IMAGE,
            "-c",
            "for c in node npm pnpm uv pip gcc cc make curl wget ssh; do"
            ' command -v "$c"; done; ls -A /app; ls -A /app/web',
            check=False,
        )
        expect(
            tools.split()
            == [
                ".venv",
                "web",
                "200.html",
                "404.html",
                "_nuxt",
                "index.html",
            ],
            tools,
        )
        distributions = run(
            "docker",
            "run",
            "--rm",
            "--entrypoint",
            "python",
            IMAGE,
            "-c",
            "import importlib.metadata as m;"
            "print('\\n'.join(sorted(d.metadata['Name'].lower() for d in m.distributions())))",
        ).split()
        forbidden = {
            "pytest",
            "mypy",
            "pyright",
            "ruff",
            "httpx",
            "hatchling",
            "pre-commit",
        }
        expect(not forbidden & set(distributions), distributions)
        passed(
            f"image {size / 1e6:.1f} MB; no Node/uv/pip/compiler/curl/ssh; "
            f"{len(distributions)} runtime Python distributions only"
        )
    finally:
        run("docker", "rm", "--force", git_container, check=False)
        run(*compose, "down", "--remove-orphans", check=False)
        print(f"Container smoke fixture: {fixture}")
    print(f"{len(checks)} container acceptance checks passed.")


if __name__ == "__main__":
    main()
