# Changelog

All notable changes are recorded here, following
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Changed

- **BurzBlog is now one Go binary with SQLite storage** (ADR-013). Posts are
  Markdown in a database; there are no content files on the server and no Git
  integration. The binary serves the API and the embedded panel and is also
  the terminal client. The Python package, recovery journals, Git transport
  and JSON publication export are removed.
- The browser panel is rebuilt: post list with filters, search and keyboard
  shortcuts; a split editor with a formatting toolbar and a live preview that
  looks like the published post; post settings, history with restore, a
  conflict view, a browser backup of unsaved work; and a settings page for
  passwords, connected computers, build tokens and site rebuilds.

### Added

- Full backup: `GET /api/backup.tar.gz`, **Settings → Backup** in the panel and
  `burzblog backup [dir]` download every post, drafts and archived included, as
  `<status>/<slug>.md` files with a `README.txt`. Needs a session or write
  token; export tokens are refused. History stays in the database file.
- Terminal workflow: `burzblog login`, `list`, `new`, `edit` (opens `$EDITOR`),
  `pull`, `push`, `publish`, `unpublish`, `archive`, `restore`, `open`,
  `export`, `logout`, `whoami`; host commands `serve` and `user add|passwd|list`.
- Versioned saves everywhere: stale saves are refused with the current
  version attached, and every change keeps a revision.
- Reader integration: `/api/export.tar.gz` with export-only tokens in Nuxt
  Content frontmatter shape, unknown frontmatter kept verbatim, and an optional
  deploy hook after publishing.
- Multiple accounts, API tokens with revocation, and a systemd unit for
  Raspberry Pi hosts; a 17 MB distroless container image.

### Removed

- Everything specific to the file-first 1.0.0 design: content directories,
  recovery journals, Git commit/push/pull, the Python CLI and JSON publication
  export. Fixes made to that implementation after 1.0.0 are superseded by this
  rewrite and are not listed. This is a breaking release (2.0.0): there is no
  automatic migration from 1.0.0 content directories. Push existing files with
  `burzblog push <slug>.md`; a `slug` key must match the file name, and keys
  such as `date`, `tags` and `authors` are kept as extra frontmatter.

## [1.0.0] - 2026-10-06

### Added

- File-first Markdown store: frontmatter round-trip, byte-preserving no-op saves, recovery journal for every changing save or move.
- Command-line interface for the content store.
- Browser editor: Markdown source with safe preview, explicit Save, Ctrl/Cmd+S, draft/publish metadata.
- Revision-aware conflict handling with an explicit disk-version choice; rename and archive.
- Authenticated API with one Argon2id administrator, CSRF and origin checks, terminal-only `init-admin`/`reset-admin`.
- Git: scoped commit, fetch, fast-forward update, non-forced push, branches, private HTTPS remotes via token file (ADR-010).
- Single-process production topology with hash-based CSP (ADR-011).
- Container image and hardened Compose (non-root, read-only, dropped capabilities, loopback-only port).
- Backup/restore drill and validated Caddy and nginx examples.
- Responsive admin UI (desktop, tablet, phone) with AA contrast, keyboard and dialog focus handling.

### Security and deployment boundaries

- No telemetry or external services; secrets only via private files, never argv, URLs or logs.
- Symlinks are never followed and destinations are never overwritten.
- Git child processes run without inherited environment or configuration.
- Production requires an operator-run HTTPS proxy; see `docs/security.md`.

### Known limitations

- Validation on the intended production host and first image publication are pending (#5).
- The node-forge advisory is build-time only and not shipped (#11).
- SSH and proxy Git transports are deferred (#18).
- No visual editor, file watcher, ARM image or automatic update.

Releases are tagged `vX.Y.Z`; the container image for a release is published as `ghcr.io/weburz/burzblog:vX.Y.Z` and `sha-<commit>` and must be pinned by digest (see `docs/release.md`).
