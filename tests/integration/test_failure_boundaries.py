import os
import stat

import pytest

from burzblog import ContentStore, Document
from burzblog.errors import ConflictError, RecoveryError


def test_external_edit_during_staging_is_detected(tmp_path, monkeypatch):
    store = ContentStore(tmp_path)
    revision = store.create("a.md", Document.new("A", "a"))
    original_fsync = os.fsync
    changed = False

    def edit_during_sync(fd):
        nonlocal changed
        if not changed and stat.S_ISREG(os.fstat(fd).st_mode):
            changed = True
            (tmp_path / "a.md").write_text("External edit\n")
        original_fsync(fd)

    monkeypatch.setattr(os, "fsync", edit_during_sync)
    with pytest.raises(ConflictError):
        store.save("a.md", Document.new("New", "new"), revision)
    assert (tmp_path / "a.md").read_text() == "External edit\n"
    assert not list(tmp_path.glob(".burzblog-*"))


def test_archive_interrupted_before_unlink_retains_complete_copies(
    tmp_path, monkeypatch
):
    store = ContentStore(tmp_path)
    (tmp_path / ".archive").mkdir()
    revision = store.create("a.md", Document.new("A", "a"))
    source = (tmp_path / "a.md").read_bytes()
    original_unlink = os.unlink

    def fail_unlink(path, **kwargs):
        if path == "publish.md":
            raise OSError("interrupted")
        return original_unlink(path, **kwargs)

    monkeypatch.setattr(os, "unlink", fail_unlink)
    with pytest.raises(RecoveryError):
        store.archive("a.md", revision)
    copies = list((tmp_path / ".archive").iterdir())
    assert len(copies) == 1
    original = next(tmp_path.glob(".burzblog-recovery/*/original.md"))
    assert copies[0].read_bytes() == original.read_bytes() == source


def test_symlink_swap_of_parent_during_open_cannot_escape(tmp_path, monkeypatch):
    root = tmp_path / "root"
    root.mkdir()
    (root / "posts").mkdir()
    outside = tmp_path / "outside"
    outside.mkdir()
    original_open = os.open
    swapped = False

    def swap(path, flags, *args, **kwargs):
        nonlocal swapped
        if path == "posts" and not swapped:
            swapped = True
            (root / "posts").rename(root / "original-posts")
            (root / "posts").symlink_to(outside, target_is_directory=True)
        return original_open(path, flags, *args, **kwargs)

    monkeypatch.setattr(os, "open", swap)
    with pytest.raises(OSError):
        ContentStore(root).create("posts/a.md", Document.new("A", "a"))
    assert list(outside.iterdir()) == []


def test_archive_accepts_long_supported_filename(tmp_path):
    (tmp_path / ".archive").mkdir()
    store = ContentStore(tmp_path)
    name = "a" * 125 + ".md"
    revision = store.create(name, Document.new("A", "a"))
    archived = store.archive(name, revision)
    assert (tmp_path / archived).exists()
