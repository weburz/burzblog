import json
import os
import subprocess
import sys
from datetime import date, datetime
from pathlib import Path

import pytest

from burzblog.document import Document
from burzblog.errors import ConflictError, ContentError
from burzblog.publication import export_publication
from burzblog.render import render_html
from burzblog.storage import ContentStore


def published(store, path="hello.md", slug="hello", body="Public text\n"):
    document = Document.new("Hello", slug, body)
    document.metadata.update(status="published", private_notes="DO NOT EXPORT")
    store.create(path, document)


def test_export_excludes_drafts_hidden_files_paths_and_private_metadata(tmp_path):
    store = ContentStore(tmp_path)
    published(store)
    store.create("draft.md", Document.new("Secret", "secret", "PRIVATE DRAFT"))
    (tmp_path / ".archive").mkdir()
    (tmp_path / ".archive/old.md").write_text("PRIVATE ARCHIVE")
    (tmp_path / ".burzblog-recovery").mkdir()
    (tmp_path / ".burzblog-recovery/old.md").write_text("PRIVATE RECOVERY")
    result = export_publication(store)
    assert json.loads(result) == {
        "schema_version": 1,
        "articles": [{"title": "Hello", "slug": "hello", "body": "Public text\n"}],
    }
    assert result == export_publication(store)
    assert not any(
        secret in result for secret in ("PRIVATE", "DO NOT EXPORT", "hello.md")
    )


@pytest.mark.parametrize(
    "bad",
    [
        "---\nstatus: published\n---\nMissing title and slug",
        "---\nstatus: public\n---\nUnknown status",
        "---\nstatus: draft\nstatus: published\n---\nDuplicate key",
    ],
)
def test_export_rejects_invalid_inventory(tmp_path, bad):
    store = ContentStore(tmp_path)
    published(store)
    (tmp_path / "invalid.md").write_text(bad)
    with pytest.raises(ContentError):
        export_publication(store)


def test_duplicate_slug_and_empty_export_require_attention(tmp_path):
    store = ContentStore(tmp_path)
    with pytest.raises(ContentError, match="Empty"):
        export_publication(store)
    assert json.loads(export_publication(store, allow_empty=True))["articles"] == []
    published(store)
    published(store, "duplicate.md")
    with pytest.raises(ContentError, match="unique"):
        export_publication(store)


def test_export_refuses_incomplete_inventory(tmp_path):
    store = ContentStore(tmp_path)
    published(store)
    (tmp_path / "unsupported name.md").write_text("unknown")
    with pytest.raises(ContentError, match="complete"):
        export_publication(store)


def test_export_detects_concurrent_edit(tmp_path, monkeypatch):
    store = ContentStore(tmp_path)
    published(store)
    original = store.read

    def changing_read(path):
        result = original(path)
        (tmp_path / path).write_text("Changed externally")
        return result

    monkeypatch.setattr(store, "read", changing_read)
    with pytest.raises(ConflictError):
        export_publication(store)


def test_export_has_a_total_size_budget(tmp_path, monkeypatch):
    store = ContentStore(tmp_path)
    published(store)
    monkeypatch.setattr("burzblog.publication.MAX_EXPORT_BYTES", 128)
    with pytest.raises(ContentError, match="64 MiB"):
        export_publication(store)


def test_cli_export_and_reference_reader_keep_last_release(tmp_path):
    root = tmp_path / "content"
    root.mkdir()
    store = ContentStore(root)
    published(
        store,
        body="![Alt](https://images.example.org/photo.jpg)\n\n<script>alert(1)</script>\n",
    )
    store.create("secret.md", Document.new("Private", "private", "DRAFT SECRET"))
    executable = str(Path(sys.executable).with_name("burzblog"))
    exported = subprocess.run(  # noqa: S603 - controlled fixture arguments
        [executable, "--root", str(root), "export"], capture_output=True, check=True
    )
    output = tmp_path / "release"
    script = Path(__file__).resolve().parents[2] / "examples/reader/build.py"
    command = [sys.executable, str(script), str(output)]
    built = subprocess.run(  # noqa: S603 - controlled fixture arguments
        command, input=exported.stdout, capture_output=True, check=False
    )
    assert built.returncode == 0, built.stderr
    article = (output / "hello/index.html").read_text()
    assert '<img src="https://images.example.org/photo.jpg"' in article
    assert "<script>" not in article
    assert not (output / "private").exists()
    assert "DRAFT SECRET" not in (output / "index.html").read_text()
    failed = subprocess.run(  # noqa: S603 - controlled fixture arguments
        command, input=exported.stdout, capture_output=True, check=False
    )
    assert failed.returncode == 1
    assert (output / "hello/index.html").read_text() == article
    (root / "invalid.md").write_text("---\nstatus: broken\n---\n")
    invalid = subprocess.run(  # noqa: S603
        [executable, "--root", str(root), "export"], capture_output=True, check=False
    )
    assert invalid.returncode == 1
    assert invalid.stdout == b""
    assert b"Traceback" not in invalid.stderr


def test_export_rejects_unsafe_file_and_preserves_sources(tmp_path):
    store = ContentStore(tmp_path)
    published(store)
    path = tmp_path / "hello.md"
    original = path.read_bytes()
    os.chmod(path, 0o666)  # noqa: S103 - deliberate unsafe fixture
    with pytest.raises(ContentError):
        export_publication(store)
    assert path.read_bytes() == original


def test_optional_public_metadata_dates_and_unpublish(tmp_path):
    store = ContentStore(tmp_path)
    document = Document.new("Hello", "hello", "A body\n")
    document.metadata.update(
        status="published",
        date=date(2026, 10, 22),
        updated=datetime.fromisoformat("2026-10-22T12:00:00+00:00"),
        authors=["An editor"],
        tags=["News"],
        description="Summary",
        image="https://media.example.org/cover.jpg",
    )
    revision = store.create("hello.md", document)
    article = json.loads(export_publication(store))["articles"][0]
    assert article["date"] == "2026-10-22"
    assert article["updated"] == "2026-10-22T12:00:00+00:00"
    assert article["authors"] == ["An editor"]
    assert article["image"] == "https://media.example.org/cover.jpg"
    document.metadata["status"] = "draft"
    store.save("hello.md", document, revision)
    assert json.loads(export_publication(store, allow_empty=True))["articles"] == []


def test_missing_status_and_symlink_are_never_exported(tmp_path):
    store = ContentStore(tmp_path)
    published(store)
    (tmp_path / "unclassified.md").write_text("PRIVATE WITHOUT STATUS")
    (tmp_path / "link.md").symlink_to(tmp_path / "hello.md")
    result = json.loads(export_publication(store))
    assert len(result["articles"]) == 1
    assert result["articles"][0]["slug"] == "hello"


@pytest.mark.parametrize(
    "payload",
    [
        {"schema_version": 2, "articles": []},
        {"schema_version": True, "articles": []},
        {
            "schema_version": 1,
            "articles": [{"slug": "../escape", "title": "X", "body": "X"}],
        },
        {
            "schema_version": 1,
            "articles": [
                {"slug": "same", "title": "X", "body": "X"},
                {"slug": "same", "title": "Y", "body": "Y"},
            ],
        },
    ],
)
def test_reader_rejects_bad_contract_before_creating_output(tmp_path, payload):
    script = Path(__file__).resolve().parents[2] / "examples/reader/build.py"
    output = tmp_path / "release"
    result = subprocess.run(  # noqa: S603 - controlled fixture arguments
        [sys.executable, str(script), str(output)],
        input=json.dumps(payload),
        capture_output=True,
        text=True,
        check=False,
    )
    assert result.returncode == 1
    assert not output.exists()
    assert "Traceback" not in result.stderr


def test_reader_images_do_not_change_private_preview_policy():
    markdown = '![safe](https://media.example.org/photo.jpg)\n\n![bad](javascript:alert(1))\n\n<img src="x" onerror="alert(1)">'
    public = render_html(markdown, images=True)
    assert '<img src="https://media.example.org/photo.jpg"' in public
    assert '<img src="javascript:' not in public
    assert '<img src="x"' not in public
    assert "<img" not in render_html(markdown)
