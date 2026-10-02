import os
import stat
import subprocess
from concurrent.futures import ThreadPoolExecutor

import pytest

from burzblog import ContentStore, Document, parse
from burzblog.document import MAX_BYTES
from burzblog.errors import ConflictError, ContentError


@pytest.fixture
def store(tmp_path):
    (tmp_path / "posts").mkdir()
    (tmp_path / ".archive").mkdir()
    return ContentStore(tmp_path)


def test_create_external_edit_read_and_noop_save(store):
    revision = store.create(
        "posts/hello.md", Document.new("Hello", "hello", "# Hello\n")
    )
    path = store.root / "posts/hello.md"
    assert (
        path.read_text()
        == "---\ntitle: Hello\nslug: hello\nstatus: draft\n---\n# Hello\n"
    )
    assert stat.S_IMODE(path.stat().st_mode) == 0o600
    path.write_bytes(
        b"---\r\ntitle: 'External' # keep\r\ncustom: yes\r\n---\r\nchanged"
    )
    snapshot = store.read("posts/hello.md")
    assert snapshot.revision != revision
    assert snapshot.document.metadata["custom"] == "yes"
    assert snapshot.document.body == "changed"
    before = path.stat()
    store.save("posts/hello.md", snapshot.document, snapshot.revision)
    assert path.stat().st_ino == before.st_ino
    assert path.stat().st_mtime_ns == before.st_mtime_ns


def test_save_preserves_mode_and_rejects_stale_revision(store):
    revision = store.create("a.md", Document.new("A", "a"))
    path = store.root / "a.md"
    path.chmod(0o640)
    document = store.read("a.md").document
    document.body = "new\n"
    current = store.save("a.md", document, revision)
    assert current != revision
    assert stat.S_IMODE(path.stat().st_mode) == 0o640
    with pytest.raises(ConflictError):
        store.save("a.md", Document.new("Stale", "a"), revision)
    assert store.read("a.md").document.body == "new\n"


@pytest.mark.parametrize(
    "path",
    [
        "../escape.md",
        "/tmp/escape.md",  # noqa: S108 - rejected traversal fixture; never opened
        "posts/../../escape.md",
        "posts//a.md",
        "posts/./a.md",
        "posts/..\\escape.md",
        "a\x00.md",
        "a.txt",
        ".git/config.md",
        "-option.md",
        "a/.hidden.md",
        "a%2fb.md",
        "x" * 129 + ".md",
        "/".join(["a"] * 34) + ".md",
        "λ.md",
    ],
)
def test_all_operations_reject_malicious_or_unsupported_paths(store, path):
    with pytest.raises(ContentError):
        store.create(path, Document.new("A", "a"))
    with pytest.raises(ContentError):
        store.read(path)
    with pytest.raises(ContentError):
        store.save(path, Document.new("A", "a"), "revision")
    with pytest.raises(ContentError):
        store.rename(path, "valid.md", "revision")


def test_symlink_directory_file_root_and_dangling_target_rejected(store, tmp_path):
    outside = tmp_path.parent / f"{tmp_path.name}-outside"
    outside.mkdir()
    target = outside / "secret.md"
    target.write_text("secret")
    (store.root / "link").symlink_to(outside, target_is_directory=True)
    (store.root / "secret.md").symlink_to(target)
    (store.root / "dangling.md").symlink_to(outside / "missing.md")
    root_link = outside / "root-link"
    root_link.symlink_to(store.root, target_is_directory=True)
    for path in ("link/secret.md", "secret.md"):
        with pytest.raises(OSError):
            store.read(path)
        with pytest.raises(OSError):
            store.save(path, Document.new("A", "a"), "revision")
    with pytest.raises(OSError):
        ContentStore(root_link).discover()
    with pytest.raises(ConflictError):
        store.create("dangling.md", Document.new("A", "a"))
    assert target.read_text() == "secret"
    assert not (outside / "missing.md").exists()
    assert store.discover() == []


def test_hardlinks_fifo_and_unsafe_modes_are_rejected(store):
    path = store.root / "a.md"
    path.write_text("body")
    os.link(path, store.root / "hard.md")
    with pytest.raises(ContentError):
        store.read("a.md")
    (store.root / "hard.md").unlink()
    path.chmod(0o666)
    with pytest.raises(ContentError):
        store.read("a.md")
    os.mkfifo(store.root / "fifo.md")
    with pytest.raises(ContentError):
        store.read("fifo.md")


def test_foreign_owner_is_rejected_without_requiring_root(store, monkeypatch):
    (store.root / "a.md").write_text("body")
    original = os.fstat

    def foreign(fd):
        info = original(fd)
        if stat.S_ISREG(info.st_mode):
            values = list(info)
            values[4] = os.geteuid() + 1
            return os.stat_result(values)
        return info

    monkeypatch.setattr(os, "fstat", foreign)
    with pytest.raises(ContentError, match="privately owned"):
        store.read("a.md")


def test_root_and_parent_must_not_be_writable_by_others(store):
    (store.root / "posts").chmod(0o777)
    with pytest.raises(ContentError):
        store.create("posts/a.md", Document.new("A", "a"))
    store.root.chmod(0o777)
    try:
        with pytest.raises(ContentError):
            store.discover()
    finally:
        store.root.chmod(0o700)


def test_discovery_reads_external_files_and_skips_hidden_and_nonmarkdown(store):
    (store.root / "posts/external.md").write_text("# External")
    (store.root / "z.md").write_text("# Z")
    (store.root / "notes.txt").write_text("not content")
    (store.root / ".archive/old.md").write_text("old")
    assert store.discover() == ["posts/external.md", "z.md"]
    assert store.read("posts/external.md").document.body == "# External"


def test_existing_destinations_never_overwritten(store):
    revision = store.create("a.md", Document.new("A", "a"))
    store.create("b.md", Document.new("B", "b"))
    with pytest.raises(ConflictError):
        store.create("a.md", Document.new("Changed", "a"))
    with pytest.raises(ConflictError):
        store.rename("a.md", "b.md", revision)
    assert store.read("a.md").document.metadata["title"] == "A"
    assert store.read("b.md").document.metadata["title"] == "B"


def test_rename_archive_and_manual_recovery_preserve_bytes(store):
    source = "---\ntitle: Legacy\nslug: original\n---\n# text"
    revision = store.create("a.md", parse(source))
    store.rename("a.md", "posts/renamed.md", revision)
    assert not (store.root / "a.md").exists()
    assert (store.root / "posts/renamed.md").read_text() == source
    with pytest.raises(ConflictError):
        store.archive("posts/renamed.md", "stale")
    archived = store.archive("posts/renamed.md", revision)
    assert store.discover() == []
    assert (store.root / archived).read_text() == source
    (store.root / archived).rename(store.root / "restored.md")
    assert store.read("restored.md").document.metadata["slug"] == "original"


def test_failure_before_publication_preserves_original_and_cleans_temp(
    store, monkeypatch
):
    revision = store.create("a.md", Document.new("A", "a"))
    original = (store.root / "a.md").read_bytes()

    def fail(*args, **kwargs):
        raise OSError("simulated disk failure")

    monkeypatch.setattr(os, "replace", fail)
    with pytest.raises(OSError):
        store.save("a.md", Document.new("New", "new"), revision)
    assert (store.root / "a.md").read_bytes() == original
    assert not list(store.root.glob(".burzblog-*"))


def test_fsync_failure_before_publication_preserves_original(store, monkeypatch):
    revision = store.create("a.md", Document.new("A", "a"))

    def fail(fd):
        raise OSError("simulated fsync failure")

    monkeypatch.setattr(os, "fsync", fail)
    with pytest.raises(OSError):
        store.save("a.md", Document.new("New", "new"), revision)
    assert store.read("a.md").document.metadata["title"] == "A"
    assert not list(store.root.glob(".burzblog-*"))


def test_concurrent_application_saves_have_one_winner(store):
    revision = store.create("a.md", Document.new("A", "a"))

    def save(title):
        try:
            ContentStore(store.root).save("a.md", Document.new(title, "a"), revision)
            return True
        except ConflictError:
            return False

    with ThreadPoolExecutor(max_workers=2) as executor:
        assert sorted(executor.map(save, ["One", "Two"])) == [False, True]
    assert store.read("a.md").document.metadata["title"] in ("One", "Two")


def test_invalid_utf8_and_oversized_files_remain_untouched(store):
    for data in (b"\xff", b"a" * (MAX_BYTES + 1), b"---\ntitle: [broken\n---\n"):
        path = store.root / "a.md"
        path.write_bytes(data)
        with pytest.raises(ContentError):
            store.read("a.md")
        assert path.read_bytes() == data


def test_ordinary_git_remains_usable_and_noop_has_empty_diff(store):
    def git(*args):
        return subprocess.run(  # noqa: S603 - controlled test repository and arguments
            ["git", "-C", str(store.root), *args],  # noqa: S607 - developer Git installation
            check=True,
            capture_output=True,
            text=True,
        )

    source = "---\ntitle: 'External' # comment\nweight: 4\n---\n\n# Body\n"
    (store.root / "external.md").write_text(source)
    git("init", "--initial-branch=main")
    git("add", "external.md")
    git(
        "-c",
        "user.name=Test",
        "-c",
        "user.email=test@example.invalid",
        "-c",
        "commit.gpgsign=false",
        "commit",
        "-m",
        "Initial content",
    )
    snapshot = store.read("external.md")
    store.save("external.md", snapshot.document, snapshot.revision)
    assert git("diff", "--exit-code").stdout == ""
    (store.root / "external.md").write_text(source + "External edit\n")
    assert "External edit" in store.read("external.md").document.body
    assert "External edit" in git("diff").stdout
