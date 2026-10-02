# Security policy and current boundary

## Authenticated application

The optional API uses one Argon2id administrator, expiring opaque sessions,
HttpOnly/SameSite cookies and exact-Origin plus session-token CSRF checks. State
must be private and outside content/Git. See [ADR-008](docs/adr/008-authenticated-application.md)
and [deployment boundaries](docs/application.md). Loopback is the only built-in
server bind; production requires a same-origin HTTPS proxy with timeouts and
connection limits. No browser/public-deployment security assurance is claimed.
Preview disables raw HTML and sanitizes allowed tags/URLs; images are omitted.
The browser adds an empty-sandbox iframe and deny-by-default preview CSP. Unsent
writing exists in memory only. See [dependency review](docs/dependencies.md) for
the known build-tool advisory and the enforced production-artifact boundary.

This is a foundation under development, not a reviewed production web service.
Supported code is the current main branch. A private reporting contact must be
chosen by the maintainers before public release. Until then, do not put secrets
or exploitable private installation details in public issues; request a private
channel from a maintainer. No response-time promise has been established.

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
retrying. See `docs/recovery.md` and ADR-005 for retention and recovery procedures.

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
Private remote credential storage remains deferred.

## Privacy

No telemetry, analytics, external fonts, tracking or remote crash reporting.
Installation contacts package registries. Git fetch/push contacts only explicitly
operator-selected endpoints; inspection never contacts remotes. Keep credentials
outside the content root. Private Git credential management is not implemented.

## Git operations

Read ADR-006/007 and `docs/git.md`. Hooks/filters/config includes are refused or
disabled. Local bare remotes with active receiver hooks are refused before Git
runs, because those hooks would execute in the application's OS account. HTTPS
endpoints must be operator-approved; do not accept arbitrary URLs from browsers.
Branch transitions apply only scoped Markdown through the recovery-aware store.
Unfinished transitions leave a persistent marker and block further Git writes.
Concurrent hostile changes to `.git` or local remote metadata are outside the
OS trust boundary; coordinate other Git clients during mutations.
