# Foundation architecture

Status: foundation with Git and authenticated API; not a complete browser CMS.

```text
Local CLI → Markdown domain → descriptor-relative filesystem store
                                      ↓
                              ordinary UTF-8 .md files
                                      ↕
                              external editor / Git
```

One Python package with independently usable content and Git domains.
Python 3.12+ provides mature tooling and POSIX directory-relative operations.
`ruamel.yaml` handles round-trip YAML; writing a YAML parser ourselves would be
less defensible. The CLI exercises the public library without introducing an
unauthenticated HTTP surface. The optional FastAPI application adds local SQLite
authentication and sanitized preview; see ADR-008 and `docs/application.md`.

## Boundaries

- `document.py`: bounded YAML parsing, schema validation, byte-preserving body
  handling and deterministic serialization; no filesystem imports.
- `storage.py`: configured root, recursive discovery, reads, versioned writes,
  no-clobber creation/relocation and reversible archival. No Git or UI imports.
- `cli.py`: local operator interface; errors without private document dumps.
- Application/authentication, Git and preview layers call this core.
  Provider credentials never belong in this package's document model.

SQLite holds authentication state, never canonical article bodies. Reads consult disk;
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
Changing saves/moves retain the actual source inode in `.burzblog-recovery`
before no-clobber publication. Recovery retains external replacements and late
writes through already-open descriptors. No-op saves do not create journals.
See ADR-005 and `docs/recovery.md`: this is a recoverable multi-step operation,
not a transaction. An interrupted mutation can leave the normal source absent.
Recovery artifacts are ordinary files, never automatically pruned or committed.

Directory descriptor confinement is not a sandbox against someone who
can rename the directory tree or change mounts. NFS/network filesystems and
native Windows are outside the tested storage contract.

## Deferred milestones

1. Nuxt UI Markdown editor and Playwright tests over the authenticated API.
2. Private remote credential handling. No visual editor until round-trip rules exist.

## Maintainer decisions

Project license, security disclosure contact, supported deployment platforms,
and eventual publishing semantics need maintainer sign-off. No license is
selected and no public service is deployed by this foundation.
