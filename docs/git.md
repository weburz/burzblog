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
hooks/custom configuration. Do not pass a browser-supplied URL to this library.
Network operations have a 30-second deadline. Redirects are not followed and TLS
certificates are always verified.

## Private HTTPS remotes

BurzBlog can fetch and push one private HTTPS repository using a username and an
access token (or password) read from a private file. It works with any host that
accepts HTTP Basic authentication over HTTPS — GitHub, GitLab, Bitbucket,
Gitea/Forgejo or your own `git http-backend` — without provider-specific code.
SSH remotes are not supported by BurzBlog; keep using ordinary Git for SSH.
See [ADR-010](adr/010-private-remote-credentials.md) for the threat model.

1. At your Git host, create the narrowest token that can read and write this
   one repository (for example a fine-grained or project access token with
   repository contents read/write). Prefer an expiry date.
2. Store it outside the content directory, its Git repository and the
   authentication state directory, readable only by the service user:

   ```sh
   install -d -m 0700 /srv/burzblog/secrets
   # Paste the token at the prompt; it never appears in shell history or argv.
   (umask 077; IFS= read -rs token && printf '%s\n' "$token" > /srv/burzblog/secrets/git-token)
   ```

   The file must be a regular file (not a symlink), owned by the service user,
   mode 0600 or 0400, one line, at most 4 KiB.
3. Configure the server environment (never put the token itself in it):

   ```sh
   export BURZBLOG_REMOTE_URL=https://git.example.org/owner/site.git
   export BURZBLOG_REMOTE_USERNAME=site-bot      # host-specific; see below
   export BURZBLOG_REMOTE_TOKEN_FILE=/srv/burzblog/secrets/git-token
   # Only for a privately signed server certificate:
   # export BURZBLOG_REMOTE_CA_FILE=/srv/burzblog/trust/internal-ca.pem
   ```

Username conventions differ by host: your account name generally works with a
personal token; GitLab project/group tokens accept any non-empty username (for
example `oauth2`), Bitbucket repository tokens use `x-token-auth`. BurzBlog sends
exactly what you configure. Usernames cannot contain `:` or spaces.

Startup refuses missing, malformed, symlinked, group/world-readable or misplaced
secret files with a specific message, and requires Git 2.31 or newer. The token
is re-read for every fetch/push, so rotating it means replacing the file
contents; no restart is needed. Revoke a token at the host to cut off access.
Back up the secret separately from content (or simply re-issue it after a
restore). Never commit it, never paste it into an issue, and never put it in a
URL: BurzBlog refuses URLs with embedded credentials.

The CLI accepts the same configuration explicitly:

```sh
uv run burzblog --root content git-push main --remote origin \
  --url https://git.example.org/owner/site.git \
  --username site-bot --token-file /srv/burzblog/secrets/git-token
```

The secret travels to Git only as a URL-scoped `Authorization` header inside the
child process environment. It is not written to `.git/config`, not passed through
argv, a credential helper or askpass program, and not returned by the API or
printed in errors. Ambient `~/.gitconfig`, `~/.git-credentials`, `~/.netrc`,
askpass and proxy variables are ignored; outbound HTTP proxies are therefore
not supported yet.

When the remote is unreachable, the certificate is untrusted, or the host rejects
the credentials, BurzBlog reports one fixed message (API status 502, code
`remote`) and changes no local content. Check the URL, network, token expiry and
scopes using ordinary Git from the same host. A push the remote refuses because
it contains newer commits or protects the branch is a separate conflict: fetch, review and reconcile
with ordinary Git. BurzBlog never force-pushes.

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
