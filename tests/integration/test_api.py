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


def test_external_replacement_after_save_does_not_report_false_success(
    client, configured, monkeypatch
):
    from burzblog.storage import ContentStore

    login(client)
    article = create(client)
    original = ContentStore.save

    def replaced_after_publication(store, path, document, revision):
        result = original(store, path, document, revision)
        (store.root / path).write_text("external after publication\n")
        return result

    monkeypatch.setattr(ContentStore, "save", replaced_after_publication)
    response = client.put(
        "/api/post",
        json={
            "path": article["path"],
            "revision": article["revision"],
            "markdown": "browser requested\n",
        },
    )
    assert response.status_code == 409
    assert (
        configured[0].content_root / article["path"]
    ).read_text() == "external after publication\n"
    assert any(
        path.read_text() == "browser requested\n"
        for path in configured[0].content_root.glob(".burzblog-recovery/*/requested.md")
    )


def test_failures_name_their_cause_without_echoing_input(client, configured):
    content = configured[0].content_root
    wrong = client.post(
        "/api/login", json={"password": "not the password"}, headers={"origin": ORIGIN}
    )
    assert wrong.status_code == 401
    assert wrong.json()["error"]["code"] == "credentials"
    assert "not the password" not in wrong.text
    foreign = client.post(
        "/api/login",
        json={"password": PASSWORD},
        headers={"origin": "https://x.invalid"},
    )
    assert foreign.status_code == 401
    assert foreign.json()["error"]["code"] == "origin"
    login(client)
    article = create(client)
    duplicate = client.post("/api/posts", json={"title": "Again", "slug": "hello"})
    assert duplicate.status_code == 409
    assert duplicate.json()["error"] == {
        "code": "exists",
        "message": "A file already exists at that path; nothing was overwritten.",
        "detail": "Destination already exists; nothing was overwritten.",
    }
    blank = client.post("/api/posts", json={"title": "   ", "slug": "blank"})
    assert blank.status_code == 400
    assert blank.json()["error"]["detail"] == "title must not be empty."
    (content / "posts/taken.md").write_text("---\ntitle: Taken\n---\n")
    moves = {
        "posts/taken.md": (409, "exists"),
        "missing/folder/new.md": (404, "not_found"),
        "posts/Bad Name.md": (400, "invalid"),
        "posts/no-extension": (400, "invalid"),
    }
    for destination, (status, code) in moves.items():
        moved = client.post(
            "/api/rename",
            json={
                "path": article["path"],
                "revision": article["revision"],
                "destination": destination,
            },
        )
        assert (moved.status_code, moved.json()["error"]["code"]) == (status, code)
        assert destination not in moved.text
    assert (content / article["path"]).read_text() == article["markdown"]
    missing = client.get("/api/post", params={"path": "posts/gone.md"})
    assert missing.status_code == 404
    assert missing.json()["error"]["code"] == "not_found"
    stale = client.put(
        "/api/post",
        json={
            "path": article["path"],
            "revision": "0" * 64,
            "markdown": "browser text\n",
        },
    )
    assert stale.json()["error"]["code"] == "conflict"
    assert "browser text" not in stale.text
    assert stale.json()["error"]["detail"].startswith("File changed externally")


def test_error_detail_refuses_path_like_messages(client, monkeypatch):
    login(client)

    def leaky(*_args):
        raise ContentError("Cannot read /srv/operator/private.md")

    monkeypatch.setattr("burzblog.application.ContentApplication.create", leaky)
    response = client.post("/api/posts", json={"title": "A", "slug": "a"})
    assert response.status_code == 400
    assert "detail" not in response.json()["error"]
    assert "/srv" not in response.text and "private.md" not in response.text


def test_list_filters_every_file_and_sorts_by_last_change(client, configured):
    login(client)
    root = configured[0].content_root
    for index in range(5):
        path = root / f"posts/p{index}.md"
        status = "published" if index % 2 else "draft"
        path.write_text(f"---\ntitle: Note {index}\nstatus: {status}\n---\n")
        os.utime(path, ns=(1_700_000_000_000_000_000 + index * 10**9,) * 2)
    (root / "posts/untitled.md").write_text("Body only\n")
    os.utime(root / "posts/untitled.md", ns=(1_600_000_000_000_000_000,) * 2)
    (root / "bad.md").write_text("---\na: [\n---\n")
    os.utime(root / "bad.md", ns=(1_500_000_000_000_000_000,) * 2)

    def listing(**params):
        response = client.get("/api/posts", params=params)
        assert response.status_code == 200, response.text
        return response.json()

    page = listing(limit=2, offset=2)
    assert (page["total"], page["files"]) == (7, 7)
    assert [item["path"] for item in page["items"]] == ["posts/p1.md", "posts/p2.md"]
    drafts = listing(status="draft", limit=1, offset=3)
    assert (drafts["total"], drafts["files"]) == (4, 7)
    assert [item["path"] for item in drafts["items"]] == ["posts/untitled.md"]
    assert listing(status="published")["total"] == 2
    assert [item["path"] for item in listing(status="unavailable")["items"]] == [
        "bad.md"
    ]
    assert [item["path"] for item in listing(q="NOTE 3")["items"]] == ["posts/p3.md"]
    untitled = listing(q="untitled")["items"]
    assert [(item["path"], item["title"]) for item in untitled] == [
        ("posts/untitled.md", "")
    ]
    newest = listing(sort="modified", limit=3)["items"]
    assert [item["path"] for item in newest] == [
        "posts/p4.md",
        "posts/p3.md",
        "posts/p2.md",
    ]
    assert newest[0]["modified"] == "2023-11-14T22:13:24+00:00"
    assert client.get("/api/posts", params={"status": "archived"}).status_code == 422
    assert client.get("/api/posts", params={"q": "x" * 201}).status_code == 422


def test_filtered_lists_reuse_only_unchanged_summaries(client, configured, monkeypatch):
    from burzblog.application import ContentApplication

    login(client)
    root = configured[0].content_root
    for index in range(3):
        (root / f"posts/n{index}.md").write_text(f"---\ntitle: Old {index}\n---\n")
    reads: list[str] = []
    original = ContentApplication.read

    def counted(application, path):
        reads.append(path)
        return original(application, path)

    monkeypatch.setattr(ContentApplication, "read", counted)

    def titles(**params):
        items = client.get("/api/posts", params=params).json()["items"]
        return [item["title"] for item in items]

    assert titles(q="old") == ["Old 0", "Old 1", "Old 2"]
    assert len(reads) == 3
    assert titles(q="old") == ["Old 0", "Old 1", "Old 2"]
    assert len(reads) == 3
    target = root / "posts/n1.md"
    before = target.stat()
    # Same size and the same modification time: only the change time differs.
    target.write_text("---\ntitle: New 1\n---\n")
    os.utime(target, ns=(before.st_atime_ns, before.st_mtime_ns))
    assert titles(q="1") == ["New 1"]
    target.chmod(0o000)
    try:
        response = client.get("/api/posts", params={"status": "unavailable"})
        assert [item["path"] for item in response.json()["items"]] == ["posts/n1.md"]
    finally:
        target.chmod(0o600)
    (root / "posts/n2.md").unlink()
    assert titles(q="old") == ["Old 0"]


def test_unreadable_frontmatter_opens_as_text_and_can_be_repaired(client, configured):
    login(client)
    root = configured[0].content_root
    broken = root / "posts/broken.md"
    text = "---\ntitle: [unterminated\nstatus: draft\n---\nBody kept\n"
    broken.write_text(text)
    opened = client.get("/api/post", params={"path": "posts/broken.md"})
    assert opened.status_code == 200
    article = opened.json()
    assert (article["markdown"], article["status"], article["title"]) == (
        text,
        "unavailable",
        "",
    )
    assert article["problem"] == "Malformed or duplicate-key YAML frontmatter."

    def save(markdown):
        return client.put(
            "/api/post",
            json={
                "path": "posts/broken.md",
                "revision": article["revision"],
                "markdown": markdown,
            },
        )

    refused = save("---\ntitle: [still broken\n---\nBody kept\n")
    assert refused.status_code == 400
    assert refused.json()["error"]["detail"] == (
        "Malformed or duplicate-key YAML frontmatter."
    )
    assert broken.read_text() == text
    published = client.post(
        "/api/publish",
        json={"path": "posts/broken.md", "revision": article["revision"]},
    )
    assert published.status_code == 400
    fixed = save("---\ntitle: Repaired\nstatus: draft\n---\nBody kept\n")
    assert fixed.status_code == 200, fixed.text
    assert (fixed.json()["title"], fixed.json()["problem"]) == ("Repaired", None)
    assert any(
        copy.read_text() == text
        for copy in root.glob(".burzblog-recovery/*/original.md")
    )
    listed = client.get("/api/posts", params={"q": "broken"}).json()["items"]
    assert [(item["title"], item["status"]) for item in listed] == [
        ("Repaired", "draft")
    ]


def test_repair_mode_never_opens_unsafe_files_and_moves_still_report(
    client, configured
):
    login(client)
    root = configured[0].content_root
    (root / "posts/latin1.md").write_bytes(b"caf\xe9\n")
    undecodable = client.get("/api/post", params={"path": "posts/latin1.md"})
    assert undecodable.status_code == 400
    assert undecodable.json()["error"]["detail"] == "Content must be UTF-8."
    (root / "posts/broken.md").write_text("---\nstatus: maybe\n---\n")
    article = client.get("/api/post", params={"path": "posts/broken.md"}).json()
    assert article["problem"] == "status must be draft or published."
    moved = client.post(
        "/api/rename",
        json={
            "path": "posts/broken.md",
            "revision": article["revision"],
            "destination": "posts/moved.md",
        },
    )
    assert moved.status_code == 200
    assert (moved.json()["path"], moved.json()["status"]) == (
        "posts/moved.md",
        "unavailable",
    )


def test_one_hostile_file_never_breaks_the_list(client, configured):
    login(client)
    root = configured[0].content_root
    create(client)
    (root / "posts/hostile.md").write_text("---\n? {a: [1]}\n: c\n---\n")
    (root / "posts/surrogate.md").write_text('---\ntitle: "\\ud83d\\ude00 Hi"\n---\n')
    response = client.get("/api/posts")
    assert response.status_code == 200, response.text
    statuses = {item["path"]: item["status"] for item in response.json()["items"]}
    assert statuses == {
        "posts/hello.md": "draft",
        "posts/hostile.md": "unavailable",
        "posts/surrogate.md": "unavailable",
    }
    opened = client.get("/api/post", params={"path": "posts/surrogate.md"})
    assert opened.status_code == 200
    assert opened.json()["problem"] == (
        "Frontmatter contains text that is not valid Unicode."
    )


def test_preview_is_bounded_and_recovers(client, monkeypatch):
    from burzblog import preview

    login(client)
    monkeypatch.setattr(preview, "TIMEOUT_SECONDS", 1.0)
    slow = client.post("/api/preview", json={"markdown": "[" * 400000})
    assert slow.status_code == 400
    assert "time and memory limits" in slow.json()["error"]["detail"]
    fine = client.post("/api/preview", json={"markdown": "# Back *again*"})
    assert fine.status_code == 200
    assert fine.json()["html"] == "<h1>Back <em>again</em></h1>\n"


@pytest.mark.parametrize(
    "source",
    [
        "﻿---\ntitle: Café\n---\nBody\n",
        "\n---\ntitle: Late\n---\nBody\n",
        "--- \ntitle: Spaced\n---\nBody\n",
        "+++\ntitle = 'TOML'\n+++\nBody\n",
    ],
)
def test_publish_never_stacks_frontmatter_on_an_unread_block(
    client, configured, source
):
    login(client)
    path = configured[0].content_root / "posts/odd.md"
    path.write_text(source)
    article = client.get("/api/post", params={"path": "posts/odd.md"}).json()
    assert article["problem"].startswith("This file opens with frontmatter")
    response = client.post(
        "/api/publish", json={"path": "posts/odd.md", "revision": article["revision"]}
    )
    assert response.status_code == 400
    assert response.json()["error"]["detail"].startswith("This file opens with")
    assert path.read_text() == source


def test_unpublish_returns_a_post_to_draft_and_keeps_everything_else(
    client, configured
):
    login(client)
    path = configured[0].content_root / "posts/kept.md"
    path.write_text(
        "---\ntitle: Kept # note\nstatus: published\ncustom: [a, b]\n---\nBody\n"
    )
    article = client.get("/api/post", params={"path": "posts/kept.md"}).json()
    response = client.post(
        "/api/unpublish",
        json={"path": "posts/kept.md", "revision": article["revision"]},
    )
    assert response.status_code == 200, response.text
    assert response.json()["status"] == "draft"
    assert path.read_text() == (
        "---\ntitle: Kept # note\nstatus: draft\ncustom: [a, b]\n---\nBody\n"
    )
    stale = client.post(
        "/api/unpublish",
        json={"path": "posts/kept.md", "revision": article["revision"]},
    )
    assert stale.status_code == 409
    assert (
        client.post(
            "/api/unpublish", json={"path": "posts/kept.md", "revision": "0" * 64}
        ).status_code
        == 409
    )
