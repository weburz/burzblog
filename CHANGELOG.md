# Changelog

All notable changes are recorded here, following
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Posts have blog metadata: `tags` (one list for topics and categories, at most
  20 lowercase hyphenated tags), an `author` (new posts start with the creating
  account), and SEO fields `seoTitle`, `seoDescription` and `canonicalUrl`.
  They are in the API (`GET /api/posts?tag=`, `GET /api/tags`), the panel's
  post settings (a tag input, an author field and a collapsible SEO section with
  character counters), the posts list (tags on each row and a tag filter, kept
  in the address), history, and the Markdown files as `tags:`, `author:`,
  `seo: {title, description}` and `canonical:`. `burzblog list --tag` filters
  by tag. Migration 4 moves `tags`, `author`, `canonical` and `seo.title` and
  `seo.description` out of a post's extra frontmatter into the new fields, and
  leaves other `seo` keys there.
- A read-only content API for websites: `GET /api/content/posts` (paged,
  `?q=`), `GET /api/content/posts/{slug}` (JSON with the Markdown body, or the
  export's exact frontmatter document with `?format=markdown`) and
  `GET /api/content/posts/{slug}/html`, with the post's tags, author and SEO
  fields, a `?tag=` filter, and `GET /api/content/tags` listing the tags of
  published posts. Published posts only; drafts, scheduled
  and archived posts answer 404 like a missing slug. Responses have an `ETag`,
  answer `If-None-Match` with 304 and are `Cache-Control: private,
  max-age=60`. Build (export) tokens, write tokens and sessions can call it.
- Renaming a post's slug keeps the old one as a redirect (migration 5, table
  `slug_redirects`). Chains collapse, so every earlier slug points at the post's
  newest one; a live slug wins over a redirect; deleting a post deletes its
  redirects. `GET /api/content/redirects` lists `{from, to}` for published posts
  only, an old slug on `GET /api/content/posts/{slug}` answers `308` with the
  current address, and the export holds the same list as `redirects.json`.
  The panel's post settings note that the old address will redirect and list a
  post's redirects with a remove button (`GET /api/posts/{slug}/redirects`,
  `DELETE /api/posts/{slug}/redirects/{from}`); `burzblog redirects [--json]`
  lists them, and `burzblog export` writes `redirects.json` next to the posts.
- `burzblog diff <file.md|slug> [file.md]` shows a unified, colored diff from
  the server's current post to a local file and says whether the file is based
  on the server's current version or an older one. It exits 0 when identical
  and 1 when different, and supports `--json`. `burzblog status [dir]` lists
  each `<slug>.md` file as up to date, modified locally, changed on server,
  both changed or not on server. A refused `push` now points to
  `burzblog diff <file>`.
- `burzblog import <file.md|dir>...` brings existing `<slug>.md` files, such as
  a Nuxt Content `content/blog/` folder or the output of `burzblog export`,
  into BurzBlog. It skips posts that already exist unless `--update` is given,
  and supports `--recursive`, `--draft`, `--dry-run` and `--json`.
- `site/install.sh`, a one-line installer for Linux and macOS
  (`curl -fsSL https://burzblog.weburz.com/install.sh | sh`). It picks the build
  for the computer, checks it against the release's `SHA256SUMS-vX.Y.Z` file and
  never calls `sudo`; `BURZBLOG_VERSION` and `BURZBLOG_INSTALL_DIR` adjust it.
  `task install:test` runs it against a local fake release server.
- The project website in `site/` (plain HTML, no build step), with a dormant
  GitHub Pages workflow and `task site:serve` to preview it.
- Public-repository files: `SECURITY.md`, `CODE_OF_CONDUCT.md`, issue and pull
  request templates, and a fuller `CONTRIBUTING.md`.

### Changed

- Build (export) tokens may now read published content through
  `GET /api/content/*` as well as the export; they are still refused on every
  other route.
- The first-run posts page hides the filters and search box until there is
  something to filter, and the panel's phone layout, row dates, dialog headers,
  and the wording for schedule, delete and the command palette were tidied.
- The design document moved from `docs/superpowers/specs/` to `docs/design/`,
  and examples and tests use neutral names.
- Help texts: `--json` now lists `schedule` and `unschedule`, `serve --data`
  names `$BURZBLOG_DATA`, `pull -o` says the file must be named `<slug>.md`, and
  `login`, `open`, `schedule` and `user` gained examples.

- The post list opens fast on a large blog: word counts are stored with each
  post and revision (a migration counts the existing ones once, the first time
  the new version starts), so listing no longer reads every body. `GET
  /api/posts` takes optional `limit` and `offset` and reports `total`; the panel
  shows 100 posts at a time with "Show more". Without `limit` the API returns
  every post as before.
- `export.tar.gz` and `backup.tar.gz` read posts one at a time instead of
  holding the whole blog in memory.
- History rows lead with what changed, and show the post title only where it
  changed. Every dialog that can be dismissed has a close button, a missing page
  inside the panel explains itself and links back, long titles wrap on phones,
  and the terminal pluralizes counts ("1 draft").

### Fixed

- Saving a published post with no publish date, such as pushing a file without
  `timestamps.publishedOn`, keeps its first publish date instead of resetting it
  to the time of the save.

### Security

- The install script swaps the binary in with a rename in the target folder,
  so an interrupted install never leaves a broken `burzblog`; it only fetches
  over HTTPS, checks the version string, stops on Ctrl-C and never falls back
  to a home folder when run as root.
- `burzblog import --update` refuses a file whose frontmatter `slug` differs
  from its file name, and warns about it otherwise.
- Terminal output replaces invisible Unicode format characters, such as
  right-to-left overrides, as well as control characters.
- The website serves its own fonts and sets a strict Content Security Policy,
  so visitors' browsers contact no third party.
- Exports and backups read posts in batches, so a slow download does not hold
  a database connection.

## [2.0.0] - 2026-10-25

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

- Panel: dark mode (Light, Dark or System, applied before first paint); a
  Ctrl/Cmd+K command palette for posts, pages and actions (Ctrl/Cmd+Shift+K
  inside the editor body, where Ctrl/Cmd+K inserts a link); sorting the post
  list by edited, published, title or created date; history rows that say what
  each version changed ("+42 words", "Published"); Undo after Unpublish and
  Archive; and editor tools: image by URL, table, rule and task list buttons,
  list continuation on Enter, Tab to indent lists, a `?` shortcuts dialog and
  a focus mode.
- Accessibility: placeholder and control-border contrast meet WCAG AA in both
  themes (checked by a unit test), focus returns to the control that opened an
  overlay, route changes and empty results are announced, the editor has a
  heading, and single-key shortcuts can be turned off in Settings.
- Install guide for the terminal tool and release binaries for Linux,
  Raspberry Pi, macOS and Windows; Settings links to the download and explains
  connecting your website in two plain steps.
- Terminal automation: a global `--json` flag (`list`, `whoami`, `new`, `pull`,
  `push`, `publish`, `unpublish`, `archive`, `restore`, `backup`) with JSON
  errors on stderr, documented exit codes (2 usage, 3 conflict, 4 not found,
  5 not signed in, 6 invalid content), `new --body-file`, `push -` with
  `--slug` and `--base-version`, and a "Scripts and AI agents" guide. `new` no
  longer opens an editor when standard input is not a terminal.
- Scheduled publishing: a writer picks a future date and time and the draft
  publishes itself then, through the normal versioned update, with a deploy
  hook call like a manual publish. `POST /api/posts/{slug}/schedule` and
  `/unschedule`, `publishAt` on posts and list items, `status=scheduled` and a
  `scheduled` count on the list, a split Publish button with **Schedule…**, a
  Scheduled badge and filter in the panel, and `burzblog schedule` and
  `unschedule`. Posts that came due while the server was down publish on
  start. New migration; existing databases upgrade on start.
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

### Security

- Previews are bounded: a pre-scan refuses tables wider than 64 columns and
  other constructs that made rendering quadratic, output is capped at 8 MiB and
  at most two previews render at once (`422 preview_limit`, `503 busy`).
- Sign-in floods are shed instead of queued (`503 busy`, per-address cap) and
  expired sign-in attempt records are pruned.
- The terminal client never follows redirects, so a password or token cannot
  be carried to `http://` or another host, and it strips control characters
  from everything it prints; single-line fields refuse control characters.
- Changing a password revokes that account's terminal (write) tokens; export
  tokens can only call the export.
- Frontmatter refuses YAML merge keys and every tag outside the core schema, so
  reader builds parse it the same way.

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

Releases are tagged `vX.Y.Z`. Each GitHub release carries the binaries from `task release` (Linux amd64/arm64/arm, macOS amd64/arm64, Windows amd64) and a `SHA256SUMS` file; see [docs/terminal.md](docs/terminal.md#install). Container images are built locally with `task docker:build` (see [docs/containers.md](docs/containers.md)).
