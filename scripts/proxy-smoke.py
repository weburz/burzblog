"""Acceptance checks for a BurzBlog deployment behind an HTTPS reverse proxy.

Run on the deployment host against a DISPOSABLE deployment (it creates a post):

    BURZBLOG_PROXY_URL=https://blog.example.org \
    BURZBLOG_SMOKE_PASSWORD='administrator password' \
    uv run --frozen python scripts/proxy-smoke.py [--ca /path/to/root.crt]

`--ca` is only needed for a privately signed test certificate; without it the
system trust store must accept the proxy's certificate. Stdlib only.
"""

import argparse
import http.client
import json
import os
import socket
import ssl
import time
import urllib.parse

checks: list[str] = []


def expect(condition: object, detail: str) -> None:
    """Fail loudly; unlike assert, never stripped by python -O."""
    if not condition:
        raise RuntimeError(f"Proxy check failed: {detail}")


def passed(description: str) -> None:
    """Record and print one verified criterion."""
    checks.append(description)
    print(f"ok - {description}", flush=True)


class Client:
    """One HTTPS connection per request; cookies kept explicitly."""

    def __init__(self, url: str, context: ssl.SSLContext) -> None:
        """Target the public proxy URL."""
        parts = urllib.parse.urlsplit(url)
        self.host = parts.hostname or ""
        self.port = parts.port or 443
        self.origin = url.rstrip("/")
        self.context = context
        self.cookie = ""

    def request(
        self,
        method: str,
        path: str,
        body: object | None = None,
        headers: dict[str, str] | None = None,
    ) -> tuple[int, http.client.HTTPMessage, str]:
        """Send a request with the current session cookie, if any."""
        connection = http.client.HTTPSConnection(
            self.host, self.port, context=self.context, timeout=30
        )
        sent = dict(headers or {})
        if self.cookie:
            sent.setdefault("Cookie", self.cookie)
        data = None
        if body is not None:
            data = json.dumps(body)
            sent.setdefault("Content-Type", "application/json")
        connection.request(method, path, body=data, headers=sent)
        response = connection.getresponse()
        text = response.read().decode("utf-8", errors="replace")
        connection.close()
        return response.status, response.headers, text


def outward_address() -> str:
    """The host's primary non-loopback address (no packet is sent)."""
    with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as probe:
        probe.connect(("192.0.2.1", 9))
        return str(probe.getsockname()[0])


def main() -> None:
    """Verify proxy, TLS, cookie, origin/CSRF and exposure properties."""
    parser = argparse.ArgumentParser()
    parser.add_argument("--ca", help="PEM root for a privately signed test certificate")
    parser.add_argument("--internal-port", type=int, default=8080)
    options = parser.parse_args()
    url = os.environ["BURZBLOG_PROXY_URL"]
    password = os.environ["BURZBLOG_SMOKE_PASSWORD"]
    context = ssl.create_default_context(cafile=options.ca)
    client = Client(url, context)
    origin = client.origin

    try:
        Client(url, ssl.create_default_context()).request("GET", "/")
        expect(
            options.ca is None, "private test certificate must not be publicly trusted"
        )
    except ssl.SSLError:
        expect(options.ca is not None, "publicly trusted certificate expected")
    status, headers, shell = client.request("GET", "/")
    expect(status == 200 and '<div id="__nuxt"' in shell, f"shell status {status}")
    passed("HTTPS verifies against the expected trust root and serves the shell")

    plain = http.client.HTTPConnection(client.host, 80, timeout=10)
    plain.request("GET", "/api/session")
    redirect = plain.getresponse()
    expect(redirect.status in (301, 308), f"http status {redirect.status}")
    expect(
        (redirect.getheader("Location") or "").startswith(f"https://{client.host}"),
        "redirect target",
    )
    plain.close()
    passed("plain HTTP redirects to HTTPS")

    policy = headers.get("Content-Security-Policy", "")
    expect("script-src 'self' 'sha256-" in policy, "shell CSP")
    expect("unsafe-inline" not in policy.split("script-src", 1)[1].split(";")[0], "CSP")
    expect("max-age=" in headers.get("Strict-Transport-Security", ""), "HSTS")
    expect(headers.get("X-Frame-Options") == "DENY", "X-Frame-Options")
    expect(headers.get("X-Content-Type-Options") == "nosniff", "nosniff")
    expect(len(headers.get_all("Content-Security-Policy") or []) == 1, "single CSP")
    asset = shell.split('src="', 1)[1].split('"', 1)[0]
    status, asset_headers, _ = client.request("GET", asset)
    expect(
        status == 200 and "immutable" in asset_headers.get("Cache-Control", ""), asset
    )
    passed("security headers, HSTS and immutable assets pass through the proxy")

    for bad_origin in (None, "https://attacker.example", url.replace("https", "http")):
        sent = {"Origin": bad_origin} if bad_origin else {}
        status, _, _ = client.request(
            "POST", "/api/login", {"password": password}, sent
        )
        expect(status == 401, f"login with Origin {bad_origin} returned {status}")
    spoofed = {
        "Origin": "https://attacker.example",
        "X-Forwarded-Host": client.host,
        "X-Forwarded-Proto": "https",
        "Forwarded": f"host={client.host};proto=https",
    }
    status, _, _ = client.request("POST", "/api/login", {"password": password}, spoofed)
    expect(status == 401, "forwarded headers must not satisfy the origin check")
    passed("missing, foreign, downgraded and forwarded-spoofed origins are refused")

    status, login_headers, body = client.request(
        "POST", "/api/login", {"password": password}, {"Origin": origin}
    )
    expect(status == 200, f"login {status}")
    cookie = login_headers.get("Set-Cookie", "")
    attributes = {part.strip().lower() for part in cookie.split(";")}
    expect({"secure", "httponly", "samesite=strict", "path=/api"} <= attributes, cookie)
    client.cookie = cookie.split(";", 1)[0]
    csrf = json.loads(body)["csrf"]
    passed("login through the proxy sets a Secure, HttpOnly, SameSite=Strict cookie")

    slug = f"proxy-{int(time.time())}"
    draft = {"title": "Proxy check", "slug": slug, "body": "Through the proxy.\n"}
    for headers_without in (
        {"Origin": origin},
        {"Origin": origin, "X-CSRF-Token": "forged"},
        {"Origin": "https://attacker.example", "X-CSRF-Token": csrf},
    ):
        status, _, _ = client.request("POST", "/api/posts", draft, headers_without)
        expect(status == 401, f"mutation {headers_without} returned {status}")
    status, _, _ = client.request(
        "POST", "/api/posts", draft, {"Origin": origin, "X-CSRF-Token": csrf}
    )
    expect(status == 201, f"create {status}")
    status, _, listing = client.request("GET", "/api/posts?limit=200")
    expect(status == 200 and f"posts/{slug}.md" in listing, "listing")
    passed("CSRF token and exact Origin are both required for mutations")

    try:
        socket.create_connection(
            (outward_address(), options.internal_port), timeout=3
        ).close()
        exposed = True
    except OSError:
        exposed = False
    expect(not exposed, "internal application port reachable on a public address")
    passed(
        f"internal port {options.internal_port} is not reachable on public addresses"
    )

    status, _, _ = client.request(
        "POST", "/api/logout", {}, {"Origin": origin, "X-CSRF-Token": csrf}
    )
    expect(status == 200, "logout")
    expect(client.request("GET", "/api/posts")[0] == 401, "session revoked")
    passed("logout revokes the proxied session")
    print(f"{len(checks)} proxy acceptance checks passed.")


if __name__ == "__main__":
    main()
