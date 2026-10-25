# ADR-013: Posts live in SQLite; one Go binary is server and CLI

Accepted, 2026-10-24. Supersedes ADR-001 to ADR-012.

## Context

The file-first design (Markdown files canonical, recovery journals, optional
Git transport) came from assuming the goal was parity with a GitHub-based
workflow. The owner's actual requirements, from the 2026-10-23 review:

- the CMS only provides an interface to write posts;
- posts are stored in the database as Markdown, with no files on the server
  and no relationship to a Git repository;
- the reader site queries the server for posts;
- it must be simple enough to run on a Raspberry Pi under systemd.

The main author also needs to keep writing from a local terminal and editor.

## Decision

- **Storage:** one SQLite file (pure-Go driver, no cgo). Each post has a
  version that increases on every change; saves name the version they started
  from and are refused if it moved. Every change keeps the previous content as
  a revision.
- **One binary:** Go. `burzblog serve` runs the JSON API and the embedded
  Nuxt panel (a static SPA, built with `nuxt generate`). The same binary is the
  CLI, which talks to the API with a bearer token: files on a laptop are
  working copies, never a second source of truth.
- **Reader sites** download published posts at build time
  (`/api/export.tar.gz`, export-only tokens) in Nuxt Content frontmatter shape,
  and a deploy hook rebuilds them on publish. Sites stay static and do not
  depend on BurzBlog being up.
- **Frontmatter the author adds** but BurzBlog does not model is kept verbatim
  (`extra`) so site-specific keys survive.

## Consequences

- No Git, recovery journals, filesystem boundary rules or Python runtime.
- Backups are one SQLite file; export gives plain Markdown at any time.
- Every account can do everything until roles are designed.
- Media uploads are deferred; covers and images are URLs.
- Two languages remain: Go for the binary, TypeScript/Vue for the panel.
