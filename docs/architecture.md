# Architecture

BurzBlog is one Go binary, `burzblog`. It is the server, the browser panel it
serves, and the terminal client:

```
burzblog serve          server + embedded browser panel + SQLite (on the host)
burzblog login <url>    terminal client on an author's computer, talks to the API
burzblog new|edit|...   the terminal writing workflow
```

The panel and the terminal client are two ways into the same server. The
database holds the only copy of each post; files the terminal client writes are
working copies, never a second source of truth.

## Goals

- Writing in the browser that feels finished: post list, split editor with
  live preview, keyboard save, unsaved-change protection, clear errors.
- Writing in the terminal as easy as `git push`: `burzblog edit <slug>` opens
  `$EDITOR` on a normal `.md` file with frontmatter and saves it back.
- No silent overwrites: every save names the version it started from and is
  refused if the post moved on, in the panel and in the terminal.
- Websites stay independent: they read published posts through the content
  API or a build-time export, so a site never goes down because BurzBlog is
  down.
- Frontmatter BurzBlog does not model survives every round trip unchanged.
- One process, one binary, one SQLite file. No Python, Node or Docker at run
  time; small enough for a Raspberry Pi under systemd.

BurzBlog does not serve the public blog itself, sync folders, talk to Git, or
scale horizontally: one instance serves one blog.

## Code layout

```
cmd/burzblog/       main: wires the subcommands
internal/post/      the Post model, slug and field validation, frontmatter
                    encode/decode; no IO
internal/store/     SQLite (modernc.org/sqlite, no cgo): posts, revisions,
                    tags, redirects, search index, accounts, sessions,
                    tokens, two-factor, audit log, media records; migrations
internal/auth/      Argon2id passwords, sessions and CSRF, API tokens,
                    sign-in rate limiting, TOTP
internal/media/     upload type detection and on-disk storage
internal/render/    goldmark (CommonMark + GFM, no raw HTML) and a bluemonday
                    allowlist, with input and output limits
internal/server/    net/http handlers: the JSON API, content API, feeds,
                    media, security headers, Origin and CSRF checks, roles,
                    the embedded panel, the deploy hook, the scheduler
internal/client/    typed API client used by the terminal commands
internal/cli/       the cobra commands
web/                Nuxt 4 + Nuxt UI panel (a static SPA built with
                    `nuxt generate`), embedded into the binary
```

## Storage

One SQLite file in the data directory, opened with the pure-Go driver so a
single cross-compile covers every platform.

- **Posts** have a slug, title, description, status (`draft`, `published` or
  `archived`; a draft can be scheduled to publish itself), the Markdown body, cover, tags, author, SEO
  fields, a featured flag, and `extra`: the frontmatter keys BurzBlog does not
  model, kept exactly as written.
- **Versions and revisions.** Every change goes through one update path that
  takes the version the caller started from, refuses the save if the post has
  moved on, raises the version, and keeps the previous content as a revision.
  Revisions are never pruned automatically and can be restored.
- **Migrations** run in order at start-up and are never edited once released;
  data changes that SQL cannot express run as Go steps alongside them.
- **Secrets** are stored only as digests: session and token secrets as
  SHA-256, passwords as Argon2id, recovery codes as SHA-256.
- **Uploads** are files under `<data>/media/`, named by their content hash and
  recorded in the database.

A backup is the data directory: the database file and `media/`.

## Access

- The browser uses an HttpOnly, SameSite=Strict session cookie. Every change
  also needs a matching `Origin` header and an `X-CSRF-Token`.
- The terminal client and integrations use bearer tokens. Write tokens act as
  their owner; export tokens only read published content.
- Accounts are admins or authors; the server enforces the role on every route.
  Two-factor sign-in (TOTP) is optional per account.
- Accounts, passwords, roles and two-factor resets are managed on the host with
  `burzblog user`, never through the web.
- Security-relevant actions are written to an append-only audit log.

[security.md](security.md) has the details and the role matrix.

## Reading content from a website

A website gets published posts in one of two ways, both with an export token:

- the content API (`/api/content/*`): JSON lists and posts, rendered HTML,
  tags, redirects, feeds and a sitemap, with ETags for caching;
- the export (`/api/export.tar.gz` or `burzblog export`): published posts as
  Markdown files in Nuxt Content frontmatter shape, for static builds.

A deploy hook, if configured, is POSTed after published content changes so the
site rebuilds. [api.md](api.md) and [publishing.md](publishing.md) describe
both, including the stability promise for the content API.

## Frontmatter

The document the terminal client edits and the export contains:

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
tags: [...]
author: ...
featured: true
seo: { title: ..., description: ... }
canonical: ...
# any other keys are kept as written
---
```

The slug is the file name, not a frontmatter key.

## Deployment

`burzblog serve --addr 127.0.0.1:8080 --data /var/lib/burzblog --origin https://blog.example.com`
behind a TLS proxy, usually as the systemd unit in `deploy/`. A container image
is available but not required. [self-hosting.md](self-hosting.md) covers both.
