# HTTP API

Everything the panel and the CLI do goes through this JSON API under `/api`.
There are no unauthenticated content routes.

## Authentication

| Caller | Credential | Changes need |
|---|---|---|
| Browser panel | `HttpOnly`, `SameSite=Strict` session cookie from `POST /api/login` (`__Host-burzblog` over HTTPS) | `Origin` equal to the configured origin, and `X-CSRF-Token` from `GET /api/session` |
| CLI | `Authorization: Bearer bzb_...` write token from `POST /api/auth/token` | nothing else |
| Website build | `Authorization: Bearer bzb_...` export token created in Settings | can only call `GET /api/export.tar.gz`; `GET /api/backup.tar.gz` answers 403 |

Sessions last 12 hours. Each username gets ten sign-in attempts per 15 minutes;
a successful sign-in resets the count. There is no instance-wide limit, so one
attacker cannot lock every author out.

## Errors

Every error has the same shape. `message` says what to do; `detail` names the
specific cause when there is one.

```json
{ "error": { "code": "validation", "message": "Fix the highlighted field, then save again.", "detail": "title must not be empty." } }
```

Codes include `authentication` (401), `credentials` (401), `rate_limited`
(429), `origin`/`csrf`/`scope` (403), `not_found` (404), `validation` (422),
`slug_taken`, `conflict` and `state` (409), `version_required` (428).

A `conflict` also carries the post as it is now, so clients can show both
versions: `{ "error": {...}, "current": { ...post } }`.

## Sessions and tokens

| | |
|---|---|
| `GET /api/session` | `{authenticated, username, csrf}`; works without signing in |
| `POST /api/login` | `{username, password}` → session cookie |
| `POST /api/logout` | ends the session |
| `POST /api/auth/token` | `{username, password, name}` → write token `{id, name, scope, secret}`; the secret is returned once |
| `GET /api/tokens` | every token (without secrets) |
| `POST /api/tokens` | `{name, scope: "write" or "export"}` → new token with its secret |
| `DELETE /api/tokens/{id}` | revoke |
| `POST /api/account/password` | `{current, next}`; signs out every other browser |
| `GET /api/settings` | `{username, origin, version, deploy}` |
| `POST /api/deploy` | request a website rebuild now |

## Posts

A post:

```json
{
  "slug": "hello-world", "title": "Hello, World!", "description": "", "status": "draft",
  "body": "# Hi\n", "coverUrl": "", "coverAlt": "", "extra": "sitemap:\n  priority: 1\n",
  "publishedOn": null, "updatedOn": null,
  "createdAt": "...", "updatedAt": "...", "updatedBy": "jarmos", "version": 3
}
```

`extra` is YAML for frontmatter keys BurzBlog does not model.

| | |
|---|---|
| `GET /api/posts?status=&q=` | `{items, counts}`; items have no body; without `status`, archived posts are left out |
| `POST /api/posts` | `{title, slug?, ...}` → new draft; the slug is made from the title when omitted |
| `GET /api/posts/{slug}` | one post |
| `PUT /api/posts/{slug}` | `{version, slug, title, description, body, coverUrl, coverAlt, extra, publishedOn, updatedOn}`; `version` must be current, a new `slug` renames |
| `POST /api/posts/{slug}/publish` | `{version}`; also `unpublish`, `archive`, `restore` |
| `DELETE /api/posts/{slug}?version=N` | only archived posts; removes its history too |
| `GET /api/posts/{slug}/revisions` | earlier versions, newest first |
| `GET /api/posts/{slug}/revisions/{v}` | one earlier version in full |
| `POST /api/posts/{slug}/revisions/{v}/restore` | `{version}` → saves that content as a new version |
| `POST /api/preview` | `{markdown}` → `{html}`, sanitized |

A save that changes nothing returns the post unchanged, without a new version.
Every change keeps the previous content as a revision.

## Markdown documents

What the CLI edits: the post as frontmatter plus body (see
[publishing](publishing.md#the-file-format) for the format).

| | |
|---|---|
| `GET /api/posts/{slug}/markdown` | `text/markdown`, with `ETag: "<version>"` |
| `PUT /api/posts/{slug}/markdown` | `If-Match: "<version>"` saves; `If-None-Match: *` creates a new post |
| `GET /api/export.tar.gz` | every published post as `<slug>.md` in a gzipped tar; export tokens allowed |
| `GET /api/backup.tar.gz` | every post, drafts and archived included, as `<status>/<slug>.md` plus a `README.txt` in a gzipped tar; session or write token, not export tokens |

The backup holds current posts only, always with a `status` line (archived posts
are stored as `draft`; the folder says `archived`). Revisions are not included:
back up the database file for those (see [self-hosting](self-hosting.md#backups)).
The response is `Content-Disposition: attachment` with a dated name such as
`burzblog-backup-2026-10-25.tar.gz`, and `Cache-Control: no-store`.

## Health

`GET /healthz` → `{"status": "ok", "version": "..."}` when the database answers.
