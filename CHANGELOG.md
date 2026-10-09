# Changelog

All notable changes are recorded here, following
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Fixed

- Editor polish (#35): the preview grows with its content instead of scrolling
  inside the page, the save shortcut shows ⌘ on macOS, slugs keep accented letters
  without their accents ("Café" → `cafe`), and a successful move is confirmed.
- Repository page: changes and commit on the left, sync and branches beside them;
  no-op push and update are disabled with the reason, changes outside Markdown are
  named as the blocker, branches switch from a list, files show their change type,
  and the page shows a loading state (#32).
- Git panel: fetch follows force-pushed remotes, dotted branch names work,
  ahead/behind appear without tracking configuration, nothing-to-commit and
  duplicate branches are explained conflicts, pushing a branch without commits is
  no longer reported as a remote failure, uncommittable names are listed apart,
  and inert tool-written config keys are accepted (#43).
- A save or move that fails after capture no longer takes the post out of its
  folder: unwritable folders are refused first, and a failed publication links
  the captured file back to its own name (#42).
- Publish refuses a file whose opening frontmatter BurzBlog cannot read (BOM,
  leading blank lines, `--- ` or TOML) instead of adding a second block above it;
  the editor explains the opening lines (#41).
- Preview and frontmatter parsing have bounded cost: rendering runs in a capped
  worker process, a quadratic text join in markdown-it is replaced, and deeply
  nested frontmatter is refused in milliseconds instead of minutes (#40).
- A malformed or unencodable file, or a non-UTF-8 name in the repository, no
  longer turns the post list or the Git panel into a server error (#39).
- Failures name their cause and appear beside the control that caused them: wrong
  passwords, existing destinations, missing folders, invalid paths and titles, and
  files removed outside BurzBlog each get their own message (#28).
- The session dialog no longer claims a valid session ended; Sign out is a direct
  button (#34).
- Drafts, Published and search cover every post instead of the visible page of
  100; search also matches filenames (#29).
- A post marked Needs attention opens as its exact text with the reason and can be
  repaired and saved in the browser (#30).

### Added

- Unpublish returns a published post to draft, changing only its status (#33).
- The address reflects the view: Back, Forward, reload and bookmarks work, the
  list keeps its filters and scroll position, and the tab title names the view (#31).
- Posts lists the most recently changed files first, shows when each changed and
  can sort by path; files without a title show as Untitled (#29).

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
