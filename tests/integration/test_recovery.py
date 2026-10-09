import json
import os
import stat

import pytest

from burzblog import ContentStore, Document
from burzblog.errors import ConflictError, ContentError, RecoveryError


@pytest.fixture
def content(tmp_path):
    store = ContentStore(tmp_path)
    revision = store.create("a.md", Document.new("Initial", "initial"))
    return store, revision


def test_replacement_at_capture_boundary_is_retained(content, monkeypatch):
    store, revision = content
    rename = os.rename

    def replace_then_capture(source, target, **kwargs):
        (store.root / "outside.tmp").write_text("external replacement")
        os.replace(store.root / "outside.tmp", store.root / "a.md")
        return rename(source, target, **kwargs)

    monkeypatch.setattr(os, "rename", replace_then_capture)
    with pytest.raises(ConflictError) as refused:
        store.save("a.md", Document.new("Browser", "browser"), revision)
    assert not isinstance(refused.value, RecoveryError)
    recovery = next(store.root.glob(".burzblog-recovery/*"))
    # The captured external version is linked back to its own name, not lost and
    # not left only in the journal; the browser's text stays in requested.md.
    assert (store.root / "a.md").read_text() == "external replacement"
    assert (store.root / "a.md").stat().st_nlink == 1
    assert not (recovery / "original.md").exists()
    assert (recovery / "restored").exists()
    assert (recovery / "observed.md").read_text().startswith("---")
    assert "Browser" in (recovery / "requested.md").read_text()
    assert store.discover() == ["a.md"]


def test_open_external_descriptor_after_publication_remains_recoverable(content):
    store, revision = content
    with (store.root / "a.md").open("r+") as external:
        store.save("a.md", Document.new("Browser", "browser"), revision)
        external.seek(0)
        external.write("late external write")
        external.truncate()
        external.flush()
    assert (
        next(store.root.glob(".burzblog-recovery/*/original.md")).read_text()
        == "late external write"
    )
    assert store.read("a.md").document.metadata["title"] == "Browser"


@pytest.mark.parametrize("operation", ["save", "rename", "archive"])
def test_external_creator_wins_publication_without_overwrite(
    content, monkeypatch, operation
):
    store, revision = content
    (store.root / ".archive").mkdir()
    link = os.link

    def create_then_publish(source, target, **kwargs):
        if source == "publish.md":
            fd = os.open(
                target,
                os.O_CREAT | os.O_EXCL | os.O_WRONLY,
                0o600,
                dir_fd=kwargs["dst_dir_fd"],
            )
            with os.fdopen(fd, "w") as stream:
                stream.write("external destination")
        return link(source, target, **kwargs)

    monkeypatch.setattr(os, "link", create_then_publish)
    # A save's source name is the contested destination, so it stays in recovery;
    # a move's source name is free, so the original goes back to it.
    with pytest.raises(RecoveryError if operation == "save" else ConflictError):
        if operation == "save":
            store.save("a.md", Document.new("Browser", "browser"), revision)
        elif operation == "rename":
            store.rename("a.md", "b.md", revision)
        else:
            store.archive("a.md", revision)
    recovery = next(store.root.glob(".burzblog-recovery/*"))
    target = json.loads((recovery / "intent.json").read_text())["destination"]
    assert (store.root / target).read_text() == "external destination"
    original = recovery / "original.md" if operation == "save" else store.root / "a.md"
    assert "Initial" in original.read_text()


@pytest.mark.parametrize("after", [False, True])
def test_publication_failure_keeps_original_and_requested(content, monkeypatch, after):
    store, revision = content
    link = os.link

    def fail(source, target, **kwargs):
        if source == "publish.md":
            if after:
                link(source, target, **kwargs)
            raise OSError("publication boundary failure")
        return link(source, target, **kwargs)

    monkeypatch.setattr(os, "link", fail)
    with pytest.raises(RecoveryError if after else ConflictError):
        store.save("a.md", Document.new("Browser", "browser"), revision)
    recovery = next(store.root.glob(".burzblog-recovery/*"))
    assert "Browser" in (recovery / "requested.md").read_text()
    if after:
        # Published before the failure: never undone, the original stays retained.
        assert "Initial" in (recovery / "original.md").read_text()
        assert "Browser" in (store.root / "a.md").read_text()
    else:
        assert "Initial" in (store.root / "a.md").read_text()
        assert (recovery / "restored").exists()


def test_source_changes_during_move_are_retained(content, monkeypatch):
    store, revision = content
    rename = os.rename

    def edit_then_move(source, target, **kwargs):
        (store.root / "a.md").write_text("external source")
        return rename(source, target, **kwargs)

    monkeypatch.setattr(os, "rename", edit_then_move)
    with pytest.raises(ConflictError):
        store.rename("a.md", "b.md", revision)
    assert (store.root / "a.md").read_text() == "external source"
    assert not (store.root / "b.md").exists()


def test_completed_recovery_journals_are_private_unique_and_not_discovered(content):
    store, revision = content
    for title in ("First", "Second"):
        revision = store.save("a.md", Document.new(title, "a"), revision)
    journals = list(store.root.glob(".burzblog-recovery/*"))
    assert len(journals) == 2
    for recovery in journals:
        assert stat.S_IMODE(recovery.stat().st_mode) == 0o700
        assert stat.S_IMODE((recovery / "requested.md").stat().st_mode) == 0o600
        assert (recovery / "complete").exists()
    assert store.discover() == ["a.md"]


def test_stale_save_creates_no_journal(content):
    store, revision = content
    (store.root / "a.md").write_text("external")
    with pytest.raises(ConflictError):
        store.save("a.md", Document.new("Browser", "browser"), revision)
    assert not (store.root / ".burzblog-recovery").exists()


def test_unsafe_recovery_directory_refused_before_capture(content):
    store, revision = content
    directory = store.root / ".burzblog-recovery"
    directory.mkdir()
    directory.chmod(0o755)
    with pytest.raises(ContentError):
        store.save("a.md", Document.new("Browser", "browser"), revision)
    assert store.read("a.md").document.metadata["title"] == "Initial"


def test_recreated_source_during_move_is_not_unlinked(content, monkeypatch):
    store, revision = content
    link = os.link

    def recreate(source, target, **kwargs):
        if source == "publish.md":
            (store.root / "a.md").write_text("new external source")
        return link(source, target, **kwargs)

    monkeypatch.setattr(os, "link", recreate)
    store.rename("a.md", "b.md", revision)
    assert (store.root / "a.md").read_text() == "new external source"
    assert store.read("b.md").document.metadata["title"] == "Initial"


def test_recovery_symlink_is_not_followed(content, tmp_path):
    store, revision = content
    outside = tmp_path / "outside"
    outside.mkdir()
    (store.root / ".burzblog-recovery").symlink_to(outside, target_is_directory=True)
    with pytest.raises(OSError):
        store.save("a.md", Document.new("Browser", "browser"), revision)
    assert list(outside.iterdir()) == []
    assert store.read("a.md").document.metadata["title"] == "Initial"


def test_noop_has_no_recovery_artifacts(content):
    store, revision = content
    store.save("a.md", store.read("a.md").document, revision)
    assert not (store.root / ".burzblog-recovery").exists()


def test_unwritable_destination_is_refused_before_anything_moves(content):
    store, revision = content
    folder = store.root / "locked"
    folder.mkdir()
    folder.chmod(0o555)
    try:
        with pytest.raises(ContentError, match="cannot write to that folder"):
            store.rename("a.md", "locked/a.md", revision)
    finally:
        folder.chmod(0o755)
    assert store.read("a.md").document.metadata["title"] == "Initial"
    assert not (store.root / ".burzblog-recovery").exists()
