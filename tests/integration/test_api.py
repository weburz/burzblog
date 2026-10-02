import os
import stat

import pytest
from fastapi.testclient import TestClient

from burzblog.api import COOKIE, create_app
from burzblog.auth import Authentication
from burzblog.errors import ContentError
from burzblog.settings import Settings

PASSWORD = "fixture only password"  # noqa: S105 - ephemeral test administrator
ORIGIN = "http://localhost:3000"


@pytest.fixture
def configured(tmp_path):
    content = tmp_path / "content"
    content.mkdir()
    (content / "posts").mkdir()
    (content / ".archive").mkdir()
    settings = Settings(content, tmp_path / "state")
    auth = Authentication(settings.state_directory)
    auth.initialize(PASSWORD)
    return settings, auth


@pytest.fixture
def client(configured):
    settings, _ = configured
    with TestClient(create_app(settings), base_url=ORIGIN) as client:
        yield client


def login(client):
    response = client.post(
        "/api/login", json={"password": PASSWORD}, headers={"origin": ORIGIN}
    )
    assert response.status_code == 200
    client.headers.update({"origin": ORIGIN, "x-csrf-token": response.json()["csrf"]})
    return response


def create(client):
    response = client.post(
        "/api/posts", json={"title": "Hello", "slug": "hello", "body": "# Hello\n"}
    )
    assert response.status_code == 201, response.text
    return response.json()


def test_create_external_edit_noop_publish_rename_archive(client, configured):
    settings, _ = configured
    login(client)
    article = create(client)
    path = settings.content_root / article["path"]
    assert path.read_text() == article["markdown"]
    path.write_bytes(
        b"---\r\ntitle: External\r\ncustom: preserved\r\n---\r\nExternal body"
    )
    read = client.get("/api/post", params={"path": article["path"]}).json()
    assert read["title"] == "External"
    before = path.stat()
    saved = client.put(
        "/api/post", json={key: read[key] for key in ("path", "revision", "markdown")}
    )
    assert saved.status_code == 200
    assert path.stat().st_ino == before.st_ino
    assert path.stat().st_mtime_ns == before.st_mtime_ns
    published = client.post(
        "/api/publish", json={key: read[key] for key in ("path", "revision")}
    ).json()
    assert published["status"] == "published"
    assert "custom: preserved" in published["markdown"]
    renamed = client.post(
        "/api/rename",
        json={
            "path": read["path"],
            "revision": published["revision"],
            "destination": "posts/renamed.md",
        },
    ).json()
    assert not path.exists()
    assert renamed["path"] == "posts/renamed.md"
    archived = client.post(
        "/api/archive", json={key: renamed[key] for key in ("path", "revision")}
    )
    assert archived.status_code == 200
    assert (settings.content_root / archived.json()["path"]).is_file()
    assert client.get("/api/posts").json()["items"] == []


def test_stale_save_is_persistent_conflict_and_external_copy_survives(
    client, configured
):
    login(client)
    article = create(client)
    path = configured[0].content_root / article["path"]
    path.write_text("external edit\n")
    response = client.put(
        "/api/post",
        json={
            "path": article["path"],
            "revision": article["revision"],
            "markdown": "browser input\n",
        },
    )
    assert response.status_code == 409
    assert response.json()["error"]["code"] == "conflict"
    assert path.read_text() == "external edit\n"


@pytest.mark.parametrize(
    "method,path,payload",
    [
        ("GET", "/api/posts", None),
        ("GET", "/api/post?path=posts/a.md", None),
        ("GET", "/api/git", None),
        ("POST", "/api/posts", {"title": "A", "slug": "a"}),
        ("POST", "/api/preview", {"markdown": "private"}),
        ("POST", "/api/git", {"action": "fetch"}),
    ],
)
def test_protected_operations_require_session(client, method, path, payload):
    response = client.request(method, path, json=payload)
    assert response.status_code == 401


def test_origin_csrf_logout_and_cookie_flags(client, configured):
    response = login(client)
    cookie = response.headers["set-cookie"]
    assert (
        "HttpOnly" in cookie and "SameSite=strict" in cookie and "Path=/api" in cookie
    )
    assert (
        configured[1].database.read_bytes().find(client.cookies[COOKIE].encode()) == -1
    )
    assert client.get("/api/session").json()["authenticated"]
    for headers in (
        {"origin": "https://evil.invalid"},
        {"x-csrf-token": "wrong"},
        {"x-csrf-token": ""},
    ):
        denied = client.post(
            "/api/posts", json={"title": "A", "slug": "a"}, headers=headers
        )
        assert denied.status_code == 401
    token = client.cookies[COOKIE]
    assert client.post("/api/logout", json={}).status_code == 200
    client.cookies.set(COOKIE, token)
    assert client.get("/api/posts").status_code == 401


def test_missing_origin_login_and_validation_dont_echo_credentials(client):
    assert client.post("/api/login", json={"password": PASSWORD}).status_code == 401
    response = client.post(
        "/api/login",
        json={"password": PASSWORD, "extra": "secret"},
        headers={"origin": ORIGIN},
    )
    assert response.status_code == 422
    assert PASSWORD not in response.text and "secret" not in response.text
    assert client.get("/api/posts", headers={"host": "evil.invalid"}).status_code == 400


@pytest.mark.parametrize(
    "path",
    [
        "../outside.md",
        "/etc/passwd",
        "posts/../../outside.md",
        ".burzblog-recovery/secret.md",
        "posts/link.md",
    ],
)
def test_malicious_read_and_create_cannot_escape(client, configured, path):
    login(client)
    outside = configured[0].state_directory / "secret.md"
    outside.write_text("operator secret")
    (configured[0].content_root / "posts/link.md").symlink_to(outside)
    assert client.get("/api/post", params={"path": path}).status_code in {400, 409}
    response = client.post("/api/posts", json={"title": "A", "slug": path})
    assert response.status_code == 400
    assert outside.read_text() == "operator secret"


def test_list_pagination_malformed_files_security_headers_and_preview(
    client, configured
):
    login(client)
    create(client)
    (configured[0].content_root / "bad.md").write_text("---\na: [\n---\n")
    response = client.get("/api/posts", params={"limit": 1})
    assert response.json()["total"] == 2
    assert response.json()["items"][0]["status"] == "unavailable"
    assert response.headers["cache-control"] == "no-store"
    assert response.headers["x-content-type-options"] == "nosniff"
    assert client.get("/api/posts", params={"limit": 201}).status_code == 422
    response = client.post(
        "/api/preview",
        json={
            "markdown": "# Heading\n<script>alert(1)</script>\n![tracking](https://evil.invalid/pixel)\n[bad](javascript:alert(1))"
        },
    )
    html = response.json()["html"]
    assert "<h1>Heading</h1>" in html
    assert (
        "<script" not in html and "<img" not in html and 'href="javascript:' not in html
    )


def test_body_limit_applies_without_content_length(client):
    def chunks():
        for _ in range(4):
            yield b"x" * (1024 * 1024)

    assert client.post("/api/login", content=chunks()).status_code == 413


def test_api_requires_initialized_admin_and_private_external_state(tmp_path):
    content = tmp_path / "content"
    content.mkdir()
    with pytest.raises(ContentError, match="outside"):
        create_app(Settings(content, content / "state"))
    with pytest.raises(ContentError, match="Initialize"):
        create_app(Settings(content, tmp_path / "state"))
    assert stat.S_IMODE((tmp_path / "state").stat().st_mode) == 0o700
    assert (
        stat.S_IMODE((tmp_path / "state/authentication.sqlite3").stat().st_mode)
        == 0o600
    )
    assert os.stat(tmp_path / "state/authentication.sqlite3").st_nlink == 1


def test_plain_filesystem_git_state_and_browser_cannot_supply_remote(client):
    login(client)
    assert client.get("/api/git").json()["repository"] is None
    assert (
        client.post(
            "/api/git", json={"action": "fetch", "url": "https://evil.invalid"}
        ).status_code
        == 422
    )
    assert client.post("/api/git", json={"action": "fetch"}).status_code == 400


def test_recovery_and_io_errors_do_not_echo_sensitive_paths(client, monkeypatch):
    from burzblog.errors import RecoveryError

    login(client)

    def broken(*_args):
        raise RecoveryError("/sensitive/operator/path")

    monkeypatch.setattr("burzblog.application.ContentApplication.create", broken)
    response = client.post("/api/posts", json={"title": "A", "slug": "a"})
    assert response.status_code == 409
    assert response.json()["error"]["code"] == "recovery"
    assert "sensitive" not in response.text

    def unreadable(*_args):
        raise OSError("/sensitive/operator/path")

    monkeypatch.setattr("burzblog.application.ContentApplication.create", unreadable)
    response = client.post("/api/posts", json={"title": "A", "slug": "a"})
    assert response.json()["error"]["code"] == "filesystem"
    assert "sensitive" not in response.text


def test_https_origin_sets_secure_cookie(configured):
    settings, _ = configured
    secure = Settings(
        settings.content_root, settings.state_directory, "https://blog.example.invalid"
    )
    with TestClient(create_app(secure), base_url=secure.origin) as client:
        response = client.post(
            "/api/login", json={"password": PASSWORD}, headers={"origin": secure.origin}
        )
        assert "Secure" in response.headers["set-cookie"]
        assert client.get("/api/posts").status_code == 200
