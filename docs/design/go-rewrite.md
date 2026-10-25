# BurzBlog in Go: database-backed writing tool

Status: accepted. Supersedes the file-first design in ADR-001 to ADR-012.

## Why

The file-first design treated Markdown files and Git as canonical. That was
more machinery than the problem needs. The goals are:

- a writing interface whose only job is authoring posts;
- posts stored **in a database as Markdown** (SQLite), with no files on the
  server's disk and no Git relationship;
- something small enough to run on a Raspberry Pi under systemd;
- a split editor: Markdown on the left, preview on the right, with a proper
  form (title, description, ...) instead of a bare text area.

Authors also need to keep writing from a local terminal and editor, and the
CMS must stay small enough that anyone can tinker with it.

## What it is

One Go binary, `burzblog`:

```
burzblog serve        server + embedded browser panel + SQLite (on the host)
burzblog login        CLI on the author's laptop, talks to the server's API
burzblog new|edit|... the terminal workflow
```

The browser panel and the CLI are two ways into the same server. The database
holds the only copy of each post. Files the CLI writes on a laptop are working
copies, never a second source of truth.

## Goals

1. Browser writing that feels finished: post list, split editor, live preview,
   keyboard save, unsaved-change protection, clear status and errors.
2. Terminal writing as easy as `git push`: `burzblog edit <slug>` opens
   `$EDITOR` on a normal `.md` file with frontmatter and saves it back.
3. No silent overwrites: every post has a version; a save based on an old
   version is refused with a clear message, in both the panel and the CLI.
4. A reader site (for example a Nuxt Content site, static on Vercel)
   gets published posts as Markdown files at build time, in its own
   frontmatter shape, and can be rebuilt automatically on publish.
5. Runs as one process, one binary, one SQLite file. No Python, Node or Docker
   at runtime.

## Non-goals (for now)

- Image and video uploads (deferred). Cover images are URLs to hosted images.
- Roles and permissions. Every account can do everything.
- Serving the public blog. BurzBlog is the kitchen; reader sites are the
  restaurant.
- Two-way folder sync or Git integration of any kind.
- Multi-tenant, horizontal scaling, external databases.

## Anti-goals (failure even if it works)

- The CLI feels clunkier than `git push`. Then the database is a step backwards
  for terminal authors.
- A post edited in two places silently loses one of the edits.
- A reader site goes down because BurzBlog is down.
- Frontmatter the author wrote by hand (e.g. `sitemap`, `seo`) disappears after
  a round trip through BurzBlog.

## Architecture

```
cmd/burzblog/            main: cobra root, wires subcommands
internal/post/           domain: Post, slug and field validation, frontmatter
                         encode/decode (pure, no IO)
internal/store/          SQLite (modernc.org/sqlite, no cgo): posts,
                         revisions, users, sessions, tokens, login attempts
internal/auth/           Argon2id hashing, sessions + CSRF, API tokens,
                         login rate limiting
internal/render/         goldmark (CommonMark + GFM, no raw HTML) + bluemonday
                         allowlist for previews
internal/server/         net/http handlers, JSON API, security headers, origin
                         checks, embedded panel, deploy hook
internal/client/         typed API client used by the CLI
internal/cli/            login/new/edit/pull/push/publish/... commands
web/                     Nuxt 4 + Nuxt UI panel (ssr: false, nuxt generate);
                         its output is embedded with go:embed
```

Decided: Go (single static binary for the Pi and
for laptops). Pure-Go SQLite so one `GOOS/GOARCH` cross-compile works for
arm64. The existing Nuxt panel is already a static SPA and is kept and
reworked, not rewritten.

### Data model

`posts`: `id`, `slug` (unique, `[a-z0-9]+(-[a-z0-9]+)*`), `title`,
`description`, `status` (`draft` | `published` | `archived`), `body`
(Markdown), `cover_url`, `cover_alt`, `extra` (JSON object of frontmatter keys
BurzBlog does not model, round-tripped verbatim), `published_at`,
`created_at`, `updated_at`, `version` (integer, +1 on every change).

`revisions`: previous content of a post on every save (post id, version,
title, description, body, extra, saved_at, saved_by). Kept, never pruned
automatically; the panel can show and restore them.

`users`: `username`, Argon2id `password_hash`. Created with
`burzblog user add` on the host; no self sign-up.

`sessions`, `tokens`, `login_attempts`: only SHA-256 digests of secrets are
stored.

### API (JSON, `/api/...`)

- Auth: `POST /api/login`, `POST /api/logout`, `GET /api/session` (returns CSRF
  token). Browser uses an HttpOnly, SameSite=Strict session cookie and
  `X-CSRF-Token` on mutations, plus an Origin check.
- Tokens: `POST /api/tokens` (username + password -> bearer token, used by
  `burzblog login`), `GET /api/tokens`, `DELETE /api/tokens/{id}`. Bearer
  requests skip CSRF (no ambient credential).
- Posts: `GET /api/posts?status=&q=`, `POST /api/posts`,
  `GET /api/posts/{slug}`, `PUT /api/posts/{slug}` (requires `version`),
  `DELETE /api/posts/{slug}` (archived posts only),
  `POST /api/posts/{slug}/publish|unpublish|archive|restore`.
- `GET /api/posts/{slug}/markdown` and `PUT` of the same: the post as one
  frontmatter + body document (what the CLI edits). Version travels in the
  `If-Match` / `ETag` headers.
- `GET /api/posts/{slug}/revisions`, `POST /api/posts/{slug}/revisions/{v}/restore`.
- `POST /api/preview`: Markdown -> sanitized HTML.
- `GET /api/export.tar.gz`: published posts as `<slug>.md` files (bearer token).
- `GET /healthz`.

Errors: `{"error": {"code", "message", "detail"}}`, messages written for the
person at the keyboard. No unauthenticated content route.

### Frontmatter format

The document the CLI edits and the export emits uses the Nuxt Content shape:

```yaml
---
title: ...
description: ...
status: draft | published
timestamps:
  publishedOn: 2026-04-25T01:46:48+00:00
  updatedOn: ...
coverImage:
  url: ...
  alt: ...
# any other keys (sitemap, seo, ...) are kept as-is
---
```

`slug` comes from the file name / URL, not the frontmatter.

### Reader integration

Recommended (static stays static, reader never depends on BurzBlog uptime):

1. Reader build downloads the export:
   `curl -fsSL -H "Authorization: Bearer $BURZBLOG_TOKEN" https://blog.example/api/export.tar.gz | tar -xz -C content/blogs`
   (or `burzblog export --out content/blogs`).
2. Nuxt Content reads the local folder instead of the GitHub repository.
3. `BURZBLOG_DEPLOY_HOOK` (e.g. a Vercel deploy hook URL) is POSTed whenever
   published content changes, debounced, so publishing triggers a rebuild.

### CLI

```
burzblog login <url>          store server URL + token (0600 config file)
burzblog logout
burzblog list [--status]      table of posts
burzblog new <slug> [--title] create a draft and open it in $EDITOR
burzblog edit <slug>          pull -> $EDITOR -> push, retry-friendly on errors
burzblog pull <slug> [-o f]   write <slug>.md locally
burzblog push <file>          send a local .md back (refused if stale; --force)
burzblog publish|unpublish <slug>
burzblog export --out <dir>   published posts as files (build-time use)
burzblog serve                run the server (host)
burzblog user add|passwd      manage accounts (host, direct DB access)
```

`edit` keeps the temp file if the push fails, prints how to retry, and never
discards the author's text.

### Panel UX

- Sign-in screen; post list with search, status filter, counts, last edited,
  empty states.
- Editor: form fields (title, slug, description, cover URL + alt) above a
  resizable split view, Markdown left and live preview right (debounced
  server render); toggle to focus mode / preview only on narrow screens.
- Save with Ctrl/Cmd+S, unsaved indicator, leave-page guard, publish/unpublish
  with confirmation, conflict dialog when the server version moved.
- Revisions drawer with restore.
- Settings: change password, CLI tokens (list, revoke), deploy hook status.
- Accessible: keyboard reachable, visible focus, labelled fields, light and
  dark.

### Deployment

- `burzblog serve --addr 127.0.0.1:8080 --data /var/lib/burzblog --origin https://blog.example`
- `deploy/burzblog.service` systemd unit (DynamicUser, StateDirectory,
  hardening flags); Caddy/nginx examples for TLS.
- Optional multi-stage Dockerfile producing a tiny image; not the primary path.

## Removed

Python package, uv/pyproject, file storage, recovery journals, Git transport,
publication JSON export, the Repository panel, and the ADRs/docs that describe
them (ADRs kept, marked superseded by ADR-013).

## Builder's calls

- Users table instead of a single password (CLI login needs a name). All users are equal until RBAC exists.
- Revisions table replaces recovery journals as the safety net.
- `extra` JSON column to honour the "frontmatter must survive" anti-goal.
- Export format is the Nuxt Content shape; other shapes can come later.
- cobra for the CLI (help, completion), goldmark + bluemonday for rendering,
  modernc.org/sqlite for cgo-free builds, golang.org/x/crypto for Argon2id.
