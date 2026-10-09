"""Measure local production processes against disposable ordinary Markdown files."""

import gzip
import json
import math
import os
import platform
import socket
import statistics
import subprocess
import sys
import tempfile
import time
from pathlib import Path

import httpx

from burzblog.auth import Authentication


def available(port: int) -> None:
    """Refuse to run alongside an existing real instance on the required ports."""
    with socket.socket() as probe:
        probe.bind(("127.0.0.1", port))


def git(root: Path, *arguments: str) -> None:
    """Prepare only the newly created fixture repository."""
    subprocess.run(  # noqa: S603 - explicit controlled fixture arguments
        ["git", "-C", str(root), *arguments],  # noqa: S607 - developer Git
        check=True,
        capture_output=True,
    )


def ready(process: subprocess.Popen[bytes], url: str) -> float:
    """Measure spawn-to-HTTP readiness, with a bounded deadline."""
    start = time.perf_counter()
    with httpx.Client(trust_env=False, timeout=1) as client:
        while time.perf_counter() - start < 20:
            if process.poll() is not None:
                raise RuntimeError("Fixture process exited before readiness.")
            try:
                if client.get(url).status_code == 200:
                    return round((time.perf_counter() - start) * 1000, 2)
            except httpx.TransportError:
                pass
            time.sleep(0.01)
    raise RuntimeError("Fixture startup deadline exceeded.")


def resident(pid: int) -> float:
    """Report Linux process RSS, not an invented cross-platform memory metric."""
    for line in Path(f"/proc/{pid}/status").read_text().splitlines():
        if line.startswith("VmRSS:"):
            return round(int(line.split()[1]) / 1024, 2)
    raise RuntimeError("Linux RSS is unavailable.")


def main() -> None:
    """Print reproducible local measurements, not a deployment capacity promise."""
    available(3000)
    project = Path(__file__).resolve().parent.parent
    output = project / "web/.output"
    if not (output / "public/index.html").is_file():
        raise RuntimeError("Build the frontend first with task web:check.")
    with tempfile.TemporaryDirectory(prefix="burzblog-measure-") as temporary:
        root = Path(temporary)
        repository = root / "repo"
        posts = repository / "content/posts"
        posts.mkdir(parents=True)
        (repository / "content/.archive").mkdir()
        for index in range(1000):
            (posts / f"post-{index:04}.md").write_text(
                f"---\ntitle: Post {index}\nstatus: draft\n---\n" + "Paragraph.\n" * 40
            )
        git(repository, "init", "--initial-branch=main")
        git(repository, "config", "user.name", "Measurement fixture")
        git(repository, "config", "user.email", "fixture@example.invalid")
        git(repository, "add", "content/posts")
        git(repository, "-c", "commit.gpgsign=false", "commit", "-m", "fixture")
        Authentication(root / "state").initialize("measurement fixture password")
        environment = {
            key: value
            for key, value in os.environ.items()
            if not key.startswith("BURZBLOG_")
        } | {
            "BURZBLOG_CONTENT_ROOT": str(repository / "content"),
            "BURZBLOG_STATE_DIR": str(root / "state"),
            "BURZBLOG_ORIGIN": "http://127.0.0.1:3000",
            "BURZBLOG_WEB_ROOT": str(output / "public"),
        }
        processes: list[subprocess.Popen[bytes]] = []
        try:
            api = subprocess.Popen(
                [
                    sys.executable,
                    "-c",
                    "from burzblog.server import main; raise SystemExit(main())",
                    "serve",
                    "--port",
                    "3000",
                ],
                env=environment,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )
            processes.append(api)
            measurements: dict[str, object] = {
                "platform": f"{platform.system()} {platform.machine()}",
                "python": platform.python_version(),
                "posts": 1000,
                "samples_per_operation": 20,
                "ready_ms": ready(api, "http://127.0.0.1:3000/"),
                "idle_rss_mib": resident(api.pid),
            }
            with httpx.Client(
                base_url="http://127.0.0.1:3000",
                trust_env=False,
                timeout=15,
                headers={"Origin": "http://127.0.0.1:3000"},
            ) as client:
                response = client.post(
                    "/api/login", json={"password": "measurement fixture password"}
                )
                response.raise_for_status()
                client.headers["X-CSRF-Token"] = response.json()["csrf"]
                article = client.get(
                    "/api/post", params={"path": "posts/post-0000.md"}
                ).json()
                # The first filtered request reads every file; later ones reuse the
                # in-memory summaries of unchanged files.
                started = time.perf_counter()
                client.get(
                    "/api/posts?limit=100&status=draft&q=post"
                ).raise_for_status()
                measurements["list_filtered_cold_ms"] = round(
                    (time.perf_counter() - started) * 1000, 2
                )
                lists = {
                    "list_page_100": "/api/posts?limit=100",
                    # The browser's default view: newest first, still one parsed page.
                    "list_recent_100": "/api/posts?limit=100&sort=modified",
                    # A status or text filter parses every file to find its matches.
                    "list_filtered_all": "/api/posts?limit=100&status=draft&q=post",
                }
                for operation in (*lists, "save", "git_status"):
                    samples: list[float] = []
                    for iteration in range(21):
                        started = time.perf_counter()
                        if operation == "save":
                            response = client.put(
                                "/api/post",
                                json={
                                    "path": article["path"],
                                    "revision": article["revision"],
                                    "markdown": f"Measured write {iteration}\n",
                                },
                            )
                        else:
                            response = client.get(lists.get(operation, "/api/git"))
                        response.raise_for_status()
                        elapsed = (time.perf_counter() - started) * 1000
                        if operation == "save":
                            article = response.json()
                        if iteration:
                            samples.append(elapsed)
                    measurements[operation] = {
                        "median_ms": round(statistics.median(samples), 2),
                        "p95_ms": round(
                            sorted(samples)[math.ceil(len(samples) * 0.95) - 1], 2
                        ),
                    }
            for extension in ("js", "css", "woff2"):
                contents = [
                    path.read_bytes()
                    for path in (output / "public").rglob(f"*.{extension}")
                ]
                measurements[f"all_emitted_{extension}"] = {
                    "bytes": sum(map(len, contents)),
                    "gzip_bytes": sum(
                        len(gzip.compress(data, mtime=0)) for data in contents
                    ),
                }
            measurements["output_bytes"] = sum(
                path.stat().st_size for path in output.rglob("*") if path.is_file()
            )
            print(json.dumps(measurements, indent=2, sort_keys=True))
        finally:
            for process in reversed(processes):
                process.terminate()
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()


if __name__ == "__main__":
    main()
