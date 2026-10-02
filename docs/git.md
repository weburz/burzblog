# Git inspection

`uv run burzblog --root /path/to/content git-status` returns a fresh JSON view:
repository root, branch/detached state, staged/unstaged/untracked changes, changed
Markdown inside the configured content root, branches, remote names, upstream,
ahead/behind counts and merge/rebase/cherry-pick/revert/conflict state.
Plain directories return `repository: null` and remain fully supported.

Git status is read-only: no index refresh writes, hooks, network fetches or
recursive submodule commands. Ahead/behind uses already-known local tracking
refs and can therefore be stale relative to the server. Remote URLs are not
returned, because they may contain credentials. Recovery files may appear as
untracked repository state but never as discoverable Markdown articles.

Repository config is deliberately restricted to ordinary `core` format/filemode/
line-ending settings, user name/email, branch tracking, remote URL/refspec values,
init.defaultBranch, pull.ff/rebase and push.default. Includes, aliases, filters,
credential helpers, fsmonitor and custom commands require external Git tooling.
No existing configuration is rewritten. Unsupported repositories fail explicitly.
Linked worktrees, submodule inspection, shallow/alternate-object repositories,
non-SHA-1 extensions and concurrent hostile `.git` changes are outside this phase.

Limits: each Git subprocess has a 10-second deadline and 4-MiB stdout limit;
config parsing has a 64-KiB limit. Failure messages omit raw stderr and credentials.
Metadata traversal rejects symlinks and group/world-writable entries and stops
at 100,000 entries. Git metadata/configuration must not change adversarially
between this validation and a command; this is not a same-user process sandbox.
The operator supplies a trusted, patched Git executable. See ADR-006.

Commit, branch changes and network synchronization are the next milestone; they
are not implemented by `git-status`.
