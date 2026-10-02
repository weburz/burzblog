import json
import subprocess
import sys
from pathlib import Path

import pytest

from burzblog import ContentStore, Document
from burzblog.errors import ConflictError, ContentError
from burzblog.git_domain import GitRepository
from burzblog.git_mutations import GitChanges, Remote, branch_name
from burzblog.git_process import GitError


def git(root, *arguments, check=True):
    return subprocess.run(  # noqa: S603 - controlled local fixtures
        ["git", "-C", str(root), *arguments],  # noqa: S607 - developer Git installation
        check=check,
        capture_output=True,
        text=True,
    )


@pytest.fixture
def workspace(tmp_path):
    root = tmp_path / "repo"
    root.mkdir()
    git(root, "init", "--initial-branch=main")
    git(root, "config", "user.name", "Test")
    git(root, "config", "user.email", "test@example.invalid")
    content = root / "content"
    content.mkdir()
    (content / ".archive").mkdir()
    (content / "a.md").write_text("initial\n")
    git(root, "add", "content/a.md")
    git(root, "-c", "commit.gpgsign=false", "commit", "-m", "initial")
    return root, content, GitChanges(content)


def test_selected_commit_excludes_unrelated_index_secrets_and_recovery(workspace):
    root, content, changes = workspace
    (root / "secret.env").write_text("not for content commit")
    git(root, "add", "secret.env")
    store = ContentStore(content)
    snapshot = store.read("a.md")
    store.save("a.md", Document.new("Edited", "edited"), snapshot.revision)
    commit = changes.commit(["a.md"], "Content edit")
    assert (
        git(root, "show", "--format=", "--name-only", commit).stdout.strip()
        == "content/a.md"
    )
    assert git(root, "diff", "--cached", "--name-only").stdout.strip() == "secret.env"
    assert ".burzblog-recovery" not in git(root, "ls-tree", "-r", "HEAD").stdout


@pytest.mark.parametrize(
    "path",
    ["../secret.md", ".burzblog-recovery/entry.md", ".archive/a.md", "secret.env"],
)
def test_commit_rejects_out_of_scope_paths(workspace, path):
    _, _, changes = workspace
    with pytest.raises(ContentError):
        changes.commit([path], "bad selection")


def test_hooks_do_not_execute_on_commit(workspace):
    root, content, changes = workspace
    hook = root / ".git/hooks/pre-commit"
    hook.write_text("#!/bin/sh\ntouch hook-ran\nexit 1\n")
    hook.chmod(0o700)
    (content / "a.md").write_text("edited\n")
    changes.commit(["a.md"], "Safe commit")
    assert not (root / "hook-ran").exists()


def test_branch_switch_retains_prior_content_and_refuses_dirty(workspace):
    _, content, changes = workspace
    changes.create_branch("draft/topic")
    changes.switch("draft/topic")
    assert GitRepository(content).status().branch == "draft/topic"
    (content / "a.md").write_text("draft text\n")
    with pytest.raises(ConflictError):
        changes.switch("main")
    changes.commit(["a.md"], "Draft work")
    changes.switch("main")
    assert (content / "a.md").read_text() == "initial\n"
    assert "draft text\n" in [
        path.read_text() for path in content.glob(".burzblog-recovery/*/original.md")
    ]
    assert GitRepository(content).status().clean
    changes.switch("draft/topic")
    assert (content / "a.md").read_text() == "draft text\n"


def test_switch_handles_creations_and_deletions_through_archive(workspace):
    _, content, changes = workspace
    changes.create_branch("draft")
    changes.switch("draft")
    (content / "a.md").unlink()
    (content / "b.md").write_text("replacement\n")
    changes.commit(["a.md", "b.md"], "Replace content")
    changes.switch("main")
    assert (content / "a.md").read_text() == "initial\n"
    assert not (content / "b.md").exists()
    assert any(
        path.read_text() == "replacement\n" for path in (content / ".archive").iterdir()
    )


def test_branch_with_noncontent_changes_is_refused_before_any_write(workspace):
    root, content, changes = workspace
    git(root, "checkout", "-b", "code")
    (root / "code.py").write_text("print('hello')")
    git(root, "add", "code.py")
    git(root, "commit", "-m", "code")
    with pytest.raises(GitError, match="outside"):
        changes.switch("main")
    assert (root / "code.py").exists()
    assert not (content / ".burzblog-recovery").exists()


def test_local_remote_roundtrip_and_divergence(workspace, tmp_path):
    root, content, changes = workspace
    bare = tmp_path / "remote.git"
    bare.mkdir()
    git(bare, "init", "--bare", "--initial-branch=main")
    remote = Remote("origin", str(bare))
    changes.push(remote, "main")
    other = tmp_path / "other"
    git(tmp_path, "clone", str(bare), str(other))
    git(other, "config", "user.name", "Other")
    git(other, "config", "user.email", "other@example.invalid")
    (other / "content/a.md").write_text("remote edit\n")
    git(other, "commit", "-am", "external")
    git(other, "push")
    changes.fetch(remote)
    assert (content / "a.md").read_text() == "initial\n"
    changes.update("origin", "main")
    assert (content / "a.md").read_text() == "remote edit\n"
    assert (
        git(root, "rev-parse", "HEAD").stdout == git(other, "rev-parse", "HEAD").stdout
    )
    (content / "a.md").write_text("local divergence\n")
    changes.commit(["a.md"], "local")
    (other / "content/a.md").write_text("remote divergence\n")
    git(other, "commit", "-am", "remote")
    git(other, "push")
    changes.fetch(remote)
    with pytest.raises(ConflictError, match="fast-forward"):
        changes.update("origin", "main")
    with pytest.raises(ConflictError, match="Could not push"):
        changes.push(remote, "main")
    assert (content / "a.md").read_text() == "local divergence\n"
    assert (other / "content/a.md").read_text() == "remote divergence\n"


def test_source_race_during_transition_retains_recovery_marker(workspace, monkeypatch):
    root, content, changes = workspace
    changes.create_branch("draft")
    changes.switch("draft")
    (content / "a.md").write_text("draft\n")
    changes.commit(["a.md"], "draft")
    save = ContentStore.save

    def external_edit(store, path, document, revision):
        (content / path).write_text("external during switch\n")
        return save(store, path, document, revision)

    monkeypatch.setattr(ContentStore, "save", external_edit)
    with pytest.raises(ConflictError, match="recovery"):
        changes.switch("main")
    assert (content / "a.md").read_text() == "external during switch\n"
    assert (root / ".git/burzblog-transition.json").exists()
    assert GitRepository(content).status().operation == "recovery"
    with pytest.raises(ConflictError):
        changes.commit(["a.md"], "blocked")


@pytest.mark.parametrize("name", ["-x", "a..b", "main~1", "a//b", "$(command)", "a\n"])
def test_branch_inputs_are_not_revision_expressions(name):
    with pytest.raises(GitError):
        branch_name(name)


@pytest.mark.parametrize(
    "url",
    [
        "ext::command",
        "ssh://host/repo",
        "https://token@host/repo",
        "http://host/repo",
        "https://host/repo?token=secret",
    ],
)
def test_unapproved_remote_transports_or_secrets_are_refused(url):
    with pytest.raises(GitError):
        Remote("origin", url).protocol()


def test_local_remote_hooks_are_refused_without_execution(workspace, tmp_path):
    _, _, changes = workspace
    bare = tmp_path / "remote.git"
    bare.mkdir()
    git(bare, "init", "--bare", "--initial-branch=main")
    hook = bare / "hooks/pre-receive"
    hook.write_text("#!/bin/sh\ntouch hook-ran\nexit 1\n")
    hook.chmod(0o700)
    with pytest.raises(GitError, match="active hooks"):
        changes.push(Remote("origin", str(bare)), "main")
    assert not (bare / "hook-ran").exists()


def test_symlink_branch_content_refused_before_transition(workspace):
    root, content, changes = workspace
    git(root, "checkout", "-b", "symlink")
    (content / "a.md").unlink()
    (content / "a.md").symlink_to("/etc/passwd")
    git(root, "add", "content/a.md")
    git(root, "commit", "-m", "symlink")
    git(root, "checkout", "main")
    with pytest.raises(GitError, match="regular"):
        changes.switch("symlink")
    assert (content / "a.md").read_text() == "initial\n"
    assert not (root / ".git/burzblog-transition.json").exists()


def test_external_edit_during_planning_aborts_without_capture(workspace, monkeypatch):
    root, content, changes = workspace
    changes.create_branch("draft")
    changes.switch("draft")
    (content / "a.md").write_text("draft\n")
    changes.commit(["a.md"], "draft")
    read = ContentStore.read

    def edit_before_read(store, path):
        (content / path).write_text("external\n")
        return read(store, path)

    monkeypatch.setattr(ContentStore, "read", edit_before_read)
    with pytest.raises(ConflictError, match="preparing"):
        changes.switch("main")
    assert (content / "a.md").read_text() == "external\n"
    assert not (root / ".git/burzblog-transition.json").exists()


def test_cli_git_flow(workspace, tmp_path):
    _, content, _ = workspace
    executable = str(Path(sys.executable).with_name("burzblog"))

    def cli(*arguments):
        return subprocess.run(  # noqa: S603 - fixture-controlled arguments
            [executable, "--root", str(content), *arguments],
            check=True,
            capture_output=True,
            text=True,
        )

    cli("git-create-branch", "draft")
    cli("git-switch", "draft")
    (content / "a.md").write_text("CLI content\n")
    assert (
        len(cli("git-commit", "a.md", "--message", "CLI commit").stdout.strip()) == 40
    )
    bare = tmp_path / "remote.git"
    bare.mkdir()
    git(bare, "init", "--bare")
    cli("git-push", "draft", "--url", str(bare))
    cli("git-fetch", "--url", str(bare))
    cli("git-update", "draft")
    state = json.loads(cli("git-status").stdout)["repository"]
    assert state["branch"] == "draft"
    assert state["clean"]
