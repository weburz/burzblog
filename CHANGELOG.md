# Changelog

All notable changes are recorded here, following
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
[Semantic Versioning](https://semver.org/).

## [Unreleased]

## [2.1.0] - 2026-10-27

### Added

- The website counts visits with Umami when the repository variable
  `UMAMI_WEBSITE_ID` is set. `scripts/site-analytics.sh` adds the tracker at
  deploy time, so the ID is never committed, and it only counts visits on the
  site's own domain.
- The guides in `docs/` are on the website at
  [burzblog.weburz.com/docs](https://burzblog.weburz.com/docs/), with a sidebar
  and a contents list per page. `task site:docs` renders them; `go:check`
  fails while the committed pages are out of date or a link between guides
  points at a heading that does not exist.
- Two-factor setup shows a QR code to scan with an authenticator app, drawn in
  the browser so the secret never leaves it; the setup key stays as a fallback
  and no longer wraps inside a group. Settings lists tag labels last, so
  password and two-factor sign-in are easy to find.
- Coolify deployment. `deploy/coolify.yaml` is a Compose file for Coolify's
  Docker Compose Empty resource: Coolify generates the URL, `BURZBLOG_ORIGIN` is
  set from it, and the database lives in a named volume. See
  [docs/containers.md](docs/containers.md#coolify).
- `task image:publish` publishes a multi-arch (`linux/amd64`, `linux/arm64`)
  image to `ghcr.io/weburz/burzblog` as `X.Y.Z`, `X.Y`, `X` and `latest`, from a
  clean `vX.Y.Z` tag; `--dry-run` builds without pushing. The image
  workflow does the same from a tag.
- Opt-in update notice. With `--check-updates` (`BURZBLOG_CHECK_UPDATES=true`)
  the server checks GitHub for a newer release shortly after start and every
  24 hours; nothing is sent but a `GET` with the BurzBlog version in the
  `User-Agent`, and without the flag it never connects. Admins get
  `GET /api/updates` (authors 403 `role`), a dismissible notice in Settings and
  a dot on the Settings tab; development builds never report an update.
- `burzblog upgrade` replaces the running binary with the latest GitHub release
  (or `--version vX.Y.Z`). It downloads over https only, checks the SHA-256 sum
  from `SHA256SUMS-<version>`, swaps the file in atomically and prints the
  restart command; it never restarts or runs the download. `--check` exits `7`
  when an update exists (new exit code, documented in `docs/terminal.md`),
  `--json` is supported, downgrades need `--force`, development builds need
  `--version`, and containers are told to pull the new image.
- The database backs itself up before a schema upgrade. When a start finds the
  schema older than the binary, it writes a consistent copy
  (`VACUUM INTO`) to `<data>/backups/burzblog-schema<N>-<UTC time>.db` first
  and refuses to migrate if that fails. The newest five copies are kept.
  Restore steps are in `docs/self-hosting.md#upgrades`.
- Featured posts. A post has a `featured` flag (frontmatter `featured: true`, a
  Featured switch in the post details, a badge in the posts list). It is saved
  as a versioned change with history like any other field and survives
  push, pull, export, import and backup. The content API returns `featured` on
  every post and `GET /api/content/posts?featured=true|false` filters on it
  (anything else is a 422); `GET /api/posts` takes the same filter. A `featured`
  key that posts kept as extra frontmatter is moved into the flag when the
  database is upgraded (migration 11).
- Tag display labels. A tag stays its slug; admins can give it a label
  (`PUT/DELETE /api/tags/{tag}/label`, a Tags section in Settings, audited as
  `tag.label`, migration 12). Without one the label is derived from the slug
  (`constitutional-law` is `Constitutional Law`). `GET /api/content/tags` items
  gain `label` and content posts gain `tagLabels`, a map from each tag to its
  label; `tags` is unchanged. Post ETags change with the labels of their tags.
- The content API returns a post's unmodelled frontmatter as `extra`, a JSON
  object (`{}` when empty), on `GET /api/content/posts/{slug}` and, with
  `?include=extra`, on the list. Dates stay strings as written. A cover that is
  an upload on this server also gets `coverWidth` and `coverHeight`, left out
  when unknown. `docs/api.md` now states that `/api/content/*` and the export
  format change only additively within a major version.

- Roles and an audit log. Every account is an `admin` or an `author`. Authors
  write, edit, publish, unpublish and archive posts and manage their own
  password, two-factor sign-in and write tokens; admins also delete posts,
  manage other accounts' tokens and export tokens, see settings, rebuild the
  website, download the backup and the export, and read the audit log. The server
  answers an author's request for an admin route 403 with the error code
  `role`; a token has the role of its owner, and `GET /api/tokens` lists only
  an author's own. The matrix is in `docs/security.md`. Existing accounts and
  the account made by first-run setup become admins (migration 8);
  `burzblog user add` makes the first account an admin and later ones authors
  unless given `--role admin`, and
  `burzblog user role <name> admin|author` changes a role on the host and
  refuses to demote the last admin. `GET /api/session` reports the `role`, and
  the panel hides what an author cannot do and gives admins an Audit log page.
- An append-only audit log (migration 9): sign-ins and failed sign-ins for
  existing accounts, account creation, two-factor changes and recovery-code use,
  token create and revoke, password and role changes, publish, unpublish,
  archive, restore, schedule and delete, slug redirect removal, manual
  rebuilds and backup downloads, each with the time, the account (and token),
  the target and the connecting address. Database triggers refuse any change or
  removal of a row, and no secret is ever stored in it. Admins read it with
  `GET /api/audit` (paged), in the panel, and with `burzblog audit` on the host.
- Image and PDF uploads. `POST /api/media` (session with CSRF, or a write
  token; export tokens get 403) takes a multipart `file` and optional `alt`,
  decides the type from the file's bytes (JPEG, PNG, GIF, WebP, AVIF, PDF; SVG
  and anything else is refused), caps the size (10 MiB, `--max-upload-mb` or
  `BURZBLOG_MAX_UPLOAD_MB`), and stores it under a content-derived name in
  `<data>/media/`, deduplicating identical uploads. `GET /api/media` lists,
  `DELETE /api/media/{id}` (admins only) removes (409 `in_use` while a post mentions the file,
  unless `?force=true`). Files are served publicly, and only they, at
  `/media/{name}` with `nosniff`, a sandboxing CSP, immutable caching and PDFs
  as downloads; see the security notes. The panel has a Media page (drag and
  drop, copy address or Markdown, delete) and an upload-or-choose picker in the
  image dialog and for the cover. `backup.tar.gz` and `burzblog backup` include
  the files as `media/<name>`. Migration 10 adds the `media` table; uploads and deletes are audited. The example
  nginx and Caddy configs now allow 12 MiB request bodies.
- Search finds words in post bodies, not only titles, slugs and descriptions.
  `?q=` on `GET /api/posts` and `GET /api/content/posts`, the panel's posts
  search box and `burzblog list --search` use an FTS5 index over title,
  description, body, tags, author and slug that ignores case and accents. The
  words you type are plain words, never search syntax: all must appear and
  the last matches as a prefix. Without `sort`, the best match comes first and
  titles weigh most. Content API search results carry a plain-text `snippet`.
  Migration 5 builds the index for existing posts.
- Two-factor sign-in (TOTP, RFC 6238) for panel accounts, off by default. Turn
  it on in Settings (password, then a code from an authenticator app) to get ten
  one-time recovery codes. Sign-in then asks for a code after the password
  (`POST /api/login/totp`), codes share the per-username failure limit, a used
  code cannot be replayed, and turning it off needs the password and a code.
  `burzblog login` asks for the code (`--code`, `BURZBLOG_TOTP`), existing
  tokens keep working, and `burzblog user 2fa-reset <name>` recovers a locked-out
  account. A migration adds the `user_totp`, `recovery_codes` and
  `login_challenges` tables.
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
- Password fields in the panel (sign-in and Settings) have a Show/Hide toggle and
  warn when Caps Lock is on. Sign-in has a "Forgot your password?" note pointing
  to `burzblog user passwd <name>`, and the new-password field in Settings shows
  a live character count and a simple strength hint; the server still enforces
  the 12-character minimum.
- First-run setup in the browser. While the server has no accounts,
  `burzblog serve` logs a one-time setup link (`<origin>/setup#token=...`,
  valid for an hour, restart for a new one) next to the existing
  `burzblog user add` alternative. Opening it shows "Create the first account"
  and signs the new account in. The sign-in page now says when there are no
  accounts yet. New endpoints `GET /api/setup` and `POST /api/setup`; see the
  [API](docs/api.md) and [security model](docs/security.md#what-is-protected-and-how).

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
- The project website in `site/` (plain HTML, no build step), published by a
  GitHub Pages workflow, with `task site:serve` to preview it.
- Public-repository files: `SECURITY.md`, `CODE_OF_CONDUCT.md`, issue and pull
  request templates, and a fuller `CONTRIBUTING.md`.

- Feeds and a sitemap for the website to serve: `GET /api/content/feed.xml`
  (RSS 2.0), `feed.atom` (Atom 1.0), `feed.json` (JSON Feed 1.1) and
  `sitemap.xml`, behind the same credentials as the rest of the content API,
  with `?limit=` (up to 100) and `?tag=` on the feeds, full sanitized HTML
  bodies (a post the renderer refuses keeps its description), `ETag` and 304.
  They need the new `serve` settings `--site-url`, `--site-title`,
  `--site-description`, `--site-language`, `--post-path` (default
  `/blog/{slug}`) and `--tag-path` (or `BURZBLOG_SITE_URL` and friends) and
  answer 409 `site_url_missing` without a site URL. With a site URL the export
  archive also holds the four files, and `burzblog export` writes them next to
  the posts. `GET /api/settings` reports the `site`, and Settings → Your website
  shows the feed status.

### Changed

- CI's browser job runs in the Playwright 1.61.1 container instead of
  installing Chromium's system packages on every run, which took over ten
  minutes. Every action is on a Node 24 release, ahead of GitHub retiring
  Node 20. Dependabot opens weekly, grouped version-update PRs for Go, the
  panel, GitHub Actions and the Docker base images.
  The image workflow runs its browser gates the same way before it builds.
- `docs/architecture.md` describes how BurzBlog is built and replaces the
  older design documents. Documentation examples use `example.com`.
- Build (export) tokens may now read published content through
  `GET /api/content/*` as well as the export; they are still refused on every
  other route.
- The first-run posts page hides the filters and search box until there is
  something to filter, and the panel's phone layout, row dates, dialog headers,
  and the wording for schedule, delete and the command palette were tidied.
- Examples and tests use neutral names.
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

- The container image now ships `/data` owned by the runtime user, so a new
  Docker named volume mounted there is writable. Before, such a volume was
  root-owned and BurzBlog could not start on it. The image is also
  cross-compiled instead of emulated when built for another architecture.
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
- The website serves its own fonts and sets a strict Content Security Policy;
  the only other host it allows is the Umami server, when analytics is on.
- Exports and backups read posts in batches, so a slow download does not hold
  a database connection.

## [2.0.0] - 2026-10-25

### Changed

- **BurzBlog is now one Go binary with SQLite storage**. Posts are
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
- Git: scoped commit, fetch, fast-forward update, non-forced push, branches, private HTTPS remotes via token file.
- Single-process production topology with hash-based CSP.
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

Releases are tagged `vX.Y.Z`. Each GitHub release carries the binaries from `task release` (Linux amd64/arm64/arm, macOS amd64/arm64, Windows amd64) and a `SHA256SUMS` file; see [docs/terminal.md](docs/terminal.md#install). Container images are published to `ghcr.io/weburz/burzblog` or can be built locally with `task docker:build`; see [docs/containers.md](docs/containers.md).
