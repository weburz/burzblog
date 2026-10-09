# Security boundaries

## Authenticated application

The optional API uses one Argon2id administrator, expiring opaque sessions,
HttpOnly/SameSite cookies and exact-Origin plus session-token CSRF checks. State
must be private and outside content/Git. See [ADR-008](adr/008-authenticated-application.md)
and [deployment boundaries](application.md). Loopback is the default server
bind (containers bind inside their namespace and publish to host loopback only);
production requires a same-origin HTTPS proxy with timeouts and connection
limits. The production process serves the static interface itself with a
hash-based CSP and runs no Node.js (ADR-011). No browser/public-deployment security assurance is claimed.
Preview disables raw HTML and sanitizes allowed tags/URLs; images are omitted.
The browser adds an empty-sandbox iframe and deny-by-default preview CSP. Unsent
writing exists in memory only. See [dependency review](dependencies.md) for
the known build-tool advisory and the enforced production-artifact boundary.

BurzBlog is a single-administrator self-hosted application behind an operator-run HTTPS proxy, not a hardened multi-tenant Internet service; report suspected security defects as ordinary GitHub issues without including secrets or private installation details.

## Threat model

Treat relative content paths and file contents as untrusted. Treat the OS user,
configured root, directory ancestors, mounts and external editor processes as
trusted. Run with a dedicated unprivileged user on a local POSIX filesystem.
Do not use a repository that an untrusted process can concurrently rearrange.

Directory descriptors plus `O_NOFOLLOW` prevent ordinary symlink traversal.
Regular file, effective UID/GID, mode and single-hard-link checks limit accidental
cross-file writes. Root/subdirectories cannot be group/world writable. This is
not a sandbox against root, another process running as the same user, directory
renames by an ancestor owner, bind mounts or hostile network filesystems.

Application writers cooperate through flock on the root directory. Revisions
detect already-completed external changes. Mutations capture the actual source
inode into a private recovery journal before no-clobber publication. An external
replacement is retained; a concurrent creator wins publication safely. Late
writes through an external descriptor remain in the captured original, which
is never automatically deleted. This provides recovery, not automatic merging.
An interrupted save/move can leave the source absent; inspect the journal before
retrying. See `recovery.md` and ADR-005 for retention and recovery procedures.

Files are fsynced before publication and parent directories after changes.
Disk/fsync errors after publication mean the result may already be visible;
inspect the file before retrying. Killed processes can leave hidden staging
files containing private content. Backups are still necessary. Publications
preserve ordinary mode bits, not ACLs, xattrs or special ownership metadata;
do not use storage requiring those properties in this phase.

YAML is bounded and rejects aliases, tags, duplicate keys and excessive nesting.
Errors omit document text. CLI errors do not echo failed filenames or parse
snippets. Preview renders and sanitizes Markdown without fetching URLs; the
source remains unchanged. Authentication/session controls are described above.
Private HTTPS remote tokens are read from an operator-owned 0600 file outside
content, Git and state; see ADR-010.

## Privacy

No telemetry, analytics, external fonts, tracking or remote crash reporting.
Installation contacts package registries. Git fetch/push contacts only explicitly
operator-selected endpoints; inspection never contacts remotes. Keep credentials
outside the content root, its repository and authentication state. BurzBlog never
writes credentials into Git configuration, URLs, logs or API responses.

## Git operations

Read ADR-006/007 and `git.md`. Hooks/filters/config includes are refused or
disabled. Local bare remotes with active receiver hooks are refused before Git
runs, because those hooks would execute in the application's OS account. HTTPS
endpoints must be operator-approved; do not accept arbitrary URLs from browsers.
Private HTTPS credentials follow ADR-010: one operator-configured remote, token
file reference, URL-scoped header via the Git child environment, mandatory TLS,
no redirects, no helpers or ambient credentials, and no SSH.
Branch transitions apply only scoped Markdown through the recovery-aware store.
Unfinished transitions leave a persistent marker and block further Git writes.
Concurrent hostile changes to `.git` or local remote metadata are outside the
OS trust boundary; coordinate other Git clients during mutations.

Preview rendering runs in one long-lived worker process (`python -I -m
burzblog.render`, empty environment) capped at 1 GiB of address space and 10
seconds per render. markdown-it has inputs whose cost grows quadratically; a
render that exceeds either limit kills the worker, the request gets a 400 and the
next preview starts a new one. Frontmatter is refused before YAML scanning when
its flow nesting exceeds 32 levels or it produces more than 20,000 tokens.
