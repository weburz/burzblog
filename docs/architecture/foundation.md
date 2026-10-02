# Foundation architecture

Status: accepted for Phase 1; not a complete CMS.

```text
Local CLI → Markdown domain → descriptor-relative filesystem store
                                      ↓
                              ordinary UTF-8 .md files
                                      ↕
                              external editor / Git
```

One Python package, one runtime dependency, no network listener or database.
Python 3.12+ provides mature tooling and POSIX directory-relative operations.
`ruamel.yaml` handles round-trip YAML; writing a YAML parser ourselves would be
less defensible. The CLI exercises the public library without introducing an
unauthenticated HTTP surface. This choice does not dictate the future web UI.

## Boundaries

- `document.py`: bounded YAML parsing, schema validation, byte-preserving body
  handling and deterministic serialization; no filesystem imports.
- `storage.py`: configured root, recursive discovery, reads, versioned writes,
  no-clobber creation/relocation and reversible archival. No Git or UI imports.
- `cli.py`: local operator interface; errors without private document dumps.
- Future application/authentication, Git, preview and UI layers call this core.
  Provider credentials never belong in this package's document model.

No operational database is justified yet. Later SQLite may hold sessions and
derived indexes, never canonical article bodies. Reads always consult disk;
polling/watch-based browser updates wait until a browser interface exists.

## Storage contract

The operator creates and configures an existing, privately controlled root.
Paths are relative `.md` paths with conservative ASCII segments; discovery skips
hidden directories, symlinks and unsupported names. Nested directories must
already exist. The application creates files, not arbitrary directory trees.
Legacy unsupported names can be edited externally. No imports or rewrites run
at startup. Malformed content remains on disk and is reported on read.

Every path component is opened relative to a directory descriptor using
`O_NOFOLLOW`. Files must be regular, single-link files owned by the effective
user. Updates preserve permission bits and refuse special modes/foreign groups.
The root and parents must not be concurrently renamed by an untrusted OS user.

Application mutations serialize with an advisory flock on the root directory.
Read revisions are SHA-256 hashes; stale saves fail before replacing anything.
Writes stage a same-directory file, fsync it, then atomically publish and fsync
the directory. New destinations use hard-link publication to avoid overwrite.
Rename/archive use link-then-unlink: interruption may leave both complete copies;
they are deliberately not claimed to be multi-file atomic transactions.

POSIX has no general compare-and-replace primitive against an uncooperative
external editor. An external save between the final revision check and replace
can be lost. **Do not edit the same file concurrently through independent tools.**
Normal sequential editor/Git workflows are supported. A browser release must
revisit this boundary, with recovery copies or a stronger coordinated protocol.
Likewise, directory descriptor confinement is not a sandbox against someone who
can rename the directory tree or change mounts. NFS/network filesystems and
native Windows are outside the tested storage contract.

## Deferred milestones

1. Review the foundation and concurrency/recovery policy.
2. Read-only Git status and explicit conservative Git commands with fixture repos.
3. Authentication and sanitized preview, then a small Markdown web editor and
   Playwright acceptance tests. No visual editor until round-trip rules exist.

## Maintainer decisions

Project license, security disclosure contact, supported deployment platforms,
and eventual publishing semantics need maintainer sign-off. No license is
selected and no public service is deployed by this foundation.
