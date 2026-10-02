"""Disposable authenticated HTTPS Git server: real TLS in front of git http-backend.

Used by integration and browser acceptance tests so private-remote behavior is
exercised without a real hosting provider or token. Every request is recorded
with its Authorization header so tests can prove where credentials were sent.
"""

from __future__ import annotations

import base64
import os
import ssl
import subprocess
import threading
from dataclasses import dataclass, field
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path


@dataclass
class Seen:
    """One request as observed by the fixture server."""

    method: str
    path: str
    authorization: str | None


@dataclass
class GitHttpsServer:
    """A running fixture; `url` is the HTTPS endpoint of the served bare repository."""

    url: str
    certificate_authority: Path
    repository: Path
    requests: list[Seen] = field(default_factory=list)


def basic(username: str, secret: str) -> str:
    """The exact header value a correctly configured client must send."""
    return "Basic " + base64.b64encode(f"{username}:{secret}".encode()).decode()


def certificate(directory: Path, prefix: str) -> tuple[Path, Path]:
    """Create a short-lived self-signed loopback certificate with OpenSSL."""
    certificate_file = directory / f"{prefix}-ca.pem"
    key = directory / f"{prefix}-key.pem"
    subprocess.run(  # noqa: S603 - fixed arguments for a disposable fixture
        [  # noqa: S607 - developer OpenSSL
            "openssl",
            "req",
            "-x509",
            "-newkey",
            "ec",
            "-pkeyopt",
            "ec_paramgen_curve:prime256v1",
            "-nodes",
            "-days",
            "1",
            "-subj",
            "/CN=127.0.0.1",
            "-addext",
            "subjectAltName=IP:127.0.0.1",
            "-keyout",
            str(key),
            "-out",
            str(certificate_file),
        ],
        check=True,
        capture_output=True,
    )
    return certificate_file, key


def _read_body(handler: BaseHTTPRequestHandler) -> bytes:
    """Read a Content-Length or chunked request body."""
    length = handler.headers.get("Content-Length")
    if length is not None:
        return handler.rfile.read(int(length))
    if handler.headers.get("Transfer-Encoding", "").lower() != "chunked":
        return b""
    body = bytearray()
    while True:
        size = int(handler.rfile.readline().split(b";")[0].strip(), 16)
        if size == 0:
            handler.rfile.readline()
            return bytes(body)
        body.extend(handler.rfile.read(size))
        handler.rfile.readline()


def start(
    directory: Path,
    expected: str | None,
    redirect: str | None = None,
    name: str = "private.git",
) -> tuple[GitHttpsServer, ThreadingHTTPServer]:
    """Serve `directory/served/<name>` over HTTPS, requiring `expected` authorization."""
    served = directory / "served"
    served.mkdir(parents=True, exist_ok=True)
    repository = served / name
    if not repository.exists():
        subprocess.run(  # noqa: S603 - fixed fixture arguments
            [  # noqa: S607 - developer Git
                "git",
                "init",
                "--quiet",
                "--bare",
                "--initial-branch=main",
                str(repository),
            ],
            check=True,
        )
    certificate_file, key = certificate(directory, name)
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.load_cert_chain(certificate_file, key)
    home = directory / "server-home"
    home.mkdir(exist_ok=True)

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, format: str, *args: object) -> None:
            """Keep test output quiet."""

        def do_GET(self) -> None:
            self._git()

        def do_POST(self) -> None:
            self._git()

        def _reply(
            self, status: int, headers: list[tuple[str, str]], payload: bytes
        ) -> None:
            self.send_response(status)
            for header, value in headers:
                self.send_header(header, value)
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)

        def _git(self) -> None:
            authorization = self.headers.get("Authorization")
            fixture.requests.append(Seen(self.command, self.path, authorization))
            if redirect is not None:
                self._reply(302, [("Location", redirect + self.path)], b"")
                return
            if expected is not None and authorization != expected:
                self._reply(401, [("WWW-Authenticate", 'Basic realm="fixture"')], b"")
                return
            body = _read_body(self)
            path, _, query = self.path.partition("?")
            environment = {
                "PATH": os.defpath,
                "HOME": str(home),
                "GIT_CONFIG_NOSYSTEM": "1",
                "GIT_CONFIG_GLOBAL": os.devnull,
                "GIT_PROJECT_ROOT": str(served),
                "GIT_HTTP_EXPORT_ALL": "1",
                "PATH_INFO": path,
                "QUERY_STRING": query,
                "REQUEST_METHOD": self.command,
                "CONTENT_TYPE": self.headers.get("Content-Type", ""),
                "CONTENT_LENGTH": str(len(body)),
                "REMOTE_USER": "fixture",
                "REMOTE_ADDR": "127.0.0.1",
            }
            if self.headers.get("Git-Protocol"):
                environment["GIT_PROTOCOL"] = self.headers["Git-Protocol"]
            if self.headers.get("Content-Encoding"):
                environment["HTTP_CONTENT_ENCODING"] = self.headers["Content-Encoding"]
            result = subprocess.run(
                ["git", "http-backend"],  # noqa: S607 - developer Git
                input=body,
                env=environment,
                capture_output=True,
                check=False,
            )
            separator = b"\r\n\r\n" if b"\r\n\r\n" in result.stdout else b"\n\n"
            head, _, payload = result.stdout.partition(separator)
            status = 200
            headers: list[tuple[str, str]] = []
            for line in head.decode("latin-1").splitlines():
                header, _, value = line.partition(":")
                if header.lower() == "status":
                    status = int(value.split()[0])
                elif header:
                    headers.append((header, value.strip()))
            self._reply(status, headers, payload)

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    server.daemon_threads = True
    server.socket = context.wrap_socket(server.socket, server_side=True)
    port = server.server_address[1]
    fixture = GitHttpsServer(
        f"https://127.0.0.1:{port}/{name}", certificate_file, repository
    )
    threading.Thread(target=server.serve_forever, daemon=True).start()
    return fixture, server
