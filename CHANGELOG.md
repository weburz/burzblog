# Changelog

All notable changes are recorded here, following
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- File-first Markdown content store with a recovery journal for every changing save or move, and a CLI.
- Authenticated API with one Argon2id administrator and local SQLite authentication state.
- Browser editor with explicit save and publish, revision-aware conflict handling, archival and safe preview.
- Scoped Git commit, fetch, fast-forward update and non-forced push, including private HTTPS remotes using private token files.
- Single-process container image with hardened Compose files, a backup/restore drill, and validated Caddy and nginx proxy examples.

### Changed

- None yet; this is the first release.

### Security

- No telemetry. Symlinks are never followed; destinations are never overwritten. Git runs without inherited environment or configuration, and raw stderr and remote URLs are never exposed.

### Known limitations

- No project license has been granted yet (issue #3).
- Production-host validation is outstanding (issue #5); one transitive advisory (node-forge, issue #11) is build-time only and not shipped.
- No visual editor, file watcher, ARM image or automatic update.

Releases are tagged `vX.Y.Z`; the container image for a release is published as `ghcr.io/weburz/burzblog:vX.Y.Z` and `sha-<commit>` and must be pinned by digest (see `docs/release.md`).
