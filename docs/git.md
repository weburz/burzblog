# Git inspection

`uv run burzblog --root /path/to/content git-status` returns a fresh JSON view:
repository root, branch/detached state, staged/unstaged/untracked changes, changed
Markdown inside the configured content root, branches, remote names, upstream,
ahead/behind counts and merge/rebase/cherry-pick/revert/conflict state.
Plain directories return `repository: null` and remain fully supported.

Git status is read-only: no index refresh writes, hooks, network fetches or
recursive submodule commands. Ahead/behind uses already-known local tracking
refs and can therefore be stale relative to the server. Remote URLs are not
returned, because they may contain credentials. Untracked `.archive` and
`.burzblog-recovery` content is excluded from application status; tracked copies
remain visible and need review.

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

## Explicit operations

```sh
uv run burzblog --root content git-create-branch draft/article
uv run burzblog --root content git-switch draft/article
uv run burzblog --root content git-commit posts/article.md --message 'Edit article'
uv run burzblog --root content git-fetch --remote origin --url https://example.org/owner/repo.git
uv run burzblog --root content git-update --remote origin draft/article
uv run burzblog --root content git-push draft/article --remote origin --url https://example.org/owner/repo.git
```

Initialize repositories and configure user.name/user.email using ordinary Git.
The CLI never invents commit identity. Only explicitly selected Markdown paths
are committed; unrelated staged changes remain staged. Recovery/archive paths
cannot be selected. No `git add .`, force push, merge or rebase is used.

Remote endpoints are operator configuration. HTTPS endpoints cannot contain
credentials, query strings or fragments. Local bare directories are supported
for private/local workflows and tests, but must be privately owned with no active
hooks/custom configuration. Private HTTPS credentials and SSH are not implemented
yet. Do not pass a browser-supplied URL to this library. HTTPS network behavior
is unverified here; acceptance tests use actual local bare repositories.
Network operations have a 30-second deadline.

Switch/update require clean user changes and apply only scoped, regular Markdown
differences. Other files, non-UTF-8/invalid Markdown, missing directories, or more
than 200 files/16 MiB are rejected before applying the transition. Updates use
already-fetched refs and must fast-forward. `.archive` must exist for deletions.
External content changes are revision-checked and retained by the file store.

## Interrupted Git transitions

If `.git/burzblog-transition.json` exists, subsequent application Git mutations
stop with a persistent recovery state. Stop writers, back up the repository,
inspect the recorded before/target commit and content paths, and compare the
ordinary recovery files described in `recovery.md`. HEAD/index may be at different
stages of the intended transition. Use an external Git client to reconcile index
and branch without overwriting recovered files; remove the marker only after
reviewing and completing recovery. Never blindly reset or retry. Do not run an
external checkout/rebase concurrently with an application Git mutation.
