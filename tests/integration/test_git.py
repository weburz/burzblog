import subprocess

import pytest

from burzblog.git_domain import GitRepository
from burzblog.git_process import GitError, run_git


@pytest.fixture
def repository(tmp_path):
    def git(*args, check=True):
        return subprocess.run(  # noqa: S603 - fixture-controlled Git arguments
            ["git", "-C", str(tmp_path), *args],  # noqa: S607 - developer Git binary
            check=check,
            capture_output=True,
            text=True,
        )

    git("init", "--initial-branch=main")
    git("config", "user.name", "Test")
    git("config", "user.email", "test@example.invalid")
    (tmp_path / "posts").mkdir()
    return tmp_path, git


def commit(root, git, content="initial"):
    (root / "posts/a.md").write_text(content)
    git("add", "posts/a.md")
    git("-c", "commit.gpgsign=false", "commit", "-m", "fixture")


def test_plain_directory_and_unborn_repository(repository, tmp_path):
    root, _ = repository
    plain = tmp_path.parent / (tmp_path.name + "-plain")
    plain.mkdir()
    assert GitRepository(plain).status() is None
    state = GitRepository(root / "posts").status()
    assert state.branch == "main"
    assert state.clean
    assert state.upstream is None


def test_staged_unstaged_untracked_and_markdown_scope(repository):
    root, git = repository
    commit(root, git)
    (root / "posts/a.md").write_text("staged")
    git("add", "posts/a.md")
    (root / "posts/a.md").write_text("unstaged")
    (root / "posts/odd name\n.md").write_text("untracked")
    (root / "outside.md").write_text("outside")
    (root / "posts/.burzblog-recovery").mkdir()
    (root / "posts/.burzblog-recovery/private.md").write_text("recovery")
    state = GitRepository(root / "posts").status()
    assert not state.clean
    change = next(item for item in state.changes if item.path == "posts/a.md")
    assert (change.index, change.worktree) == ("M", "M")
    assert set(state.changed_markdown) == {"posts/a.md", "posts/odd name\n.md"}


def test_status_does_not_refresh_index_or_disclose_remote_url(repository):
    root, git = repository
    commit(root, git)
    git("remote", "add", "origin", "https://secret-token@example.invalid/private.git")
    index = (root / ".git/index").read_bytes()
    before = (root / ".git/index").stat().st_mtime_ns
    state = GitRepository(root).status()
    assert state.remotes == ("origin",)
    assert "secret-token" not in repr(state)
    assert (root / ".git/index").read_bytes() == index
    assert (root / ".git/index").stat().st_mtime_ns == before


def test_branches_detached_and_staged_rename(repository):
    root, git = repository
    commit(root, git)
    git("branch", "draft")
    git("mv", "posts/a.md", "posts/renamed.md")
    state = GitRepository(root).status()
    assert set(state.branches) == {"main", "draft"}
    assert state.changes[0].original_path == "posts/a.md"
    git("commit", "-m", "rename")
    git("checkout", "--detach")
    assert GitRepository(root).status().detached


def test_ahead_behind_and_conflicting_merge(repository):
    root, git = repository
    commit(root, git)
    git("branch", "other")
    git("branch", "--set-upstream-to=other", "main")
    commit(root, git, "main change")
    state = GitRepository(root).status()
    assert (state.ahead, state.behind, state.upstream) == (1, 0, "other")
    git("checkout", "other")
    commit(root, git, "other change")
    git("checkout", "main")
    state = GitRepository(root).status()
    assert (state.ahead, state.behind) == (1, 1)
    git("merge", "other", check=False)
    state = GitRepository(root).status()
    assert state.operation == "merge"
    assert any(item.conflict for item in state.changes)


@pytest.mark.parametrize(
    "key",
    [
        "core.fsmonitor",
        "core.sshCommand",
        "filter.evil.clean",
        "include.path",
        "credential.helper",
        "alias.status",
        "core.worktree",
    ],
)
def test_executable_configuration_is_rejected_before_status(repository, key):
    root, git = repository
    git("config", key, "!touch should-not-exist")
    with pytest.raises(GitError, match="configuration requires review"):
        GitRepository(root).status()
    assert not (root / "should-not-exist").exists()


def test_global_config_and_git_environment_are_not_inherited(repository, monkeypatch):
    root, git = repository
    commit(root, git)
    monkeypatch.setenv("GIT_CONFIG_COUNT", "1")
    monkeypatch.setenv("GIT_CONFIG_KEY_0", "core.fsmonitor")
    monkeypatch.setenv("GIT_CONFIG_VALUE_0", "!touch should-not-exist")
    monkeypatch.setenv("GIT_DIR", "/nonexistent")
    assert GitRepository(root).status().clean
    assert not (root / "should-not-exist").exists()


def test_git_output_and_timeout_are_bounded(repository):
    root, _ = repository
    with pytest.raises(GitError, match="limit"):
        run_git(root, ["--version"], limit=1)
    with pytest.raises(GitError, match="timed out"):
        run_git(root, ["--version"], timeout=0)


def test_linked_worktree_marker_is_refused(tmp_path):
    (tmp_path / ".git").write_text("gitdir: /outside")
    with pytest.raises(GitError):
        GitRepository(tmp_path).status()


def test_metadata_symlinks_refused(repository):
    root, _ = repository
    (root / "external").write_text("ref: refs/heads/main\n")
    (root / ".git/HEAD").unlink()
    (root / ".git/HEAD").symlink_to(root / "external")
    with pytest.raises(GitError, match="metadata"):
        GitRepository(root).status()


def test_rebase_state_reported(repository):
    root, git = repository
    commit(root, git)
    (root / ".git/rebase-merge").mkdir()
    assert GitRepository(root).status().operation == "rebase"
