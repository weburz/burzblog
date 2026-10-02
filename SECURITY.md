# Security policy and current boundary

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
snippets. Markdown is never rendered or executed and URLs are never fetched.
Before a web layer exists, design authentication, session/CSRF controls, rate
limits, sanitized preview, remote URL policies and credential storage explicitly.

## Privacy

No runtime network calls, telemetry, analytics, external fonts, tracking or
remote crash reporting. Installation contacts package registries. Future Git
network operations must be opt-in. Keep credentials outside the content root.
