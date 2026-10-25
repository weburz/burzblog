# HTTP API

Everything the panel and the CLI do goes through this JSON API under `/api`.
There are no unauthenticated content routes.

## Authentication

| Caller | Credential | Changes need |
|---|---|---|
| Browser panel | `HttpOnly`, `SameSite=Strict` session cookie from `POST /api/login` (`__Host-burzblog` over HTTPS, `burzblog_session` over plain HTTP) | `Origin` equal to the configured origin, and `X-CSRF-Token` from `GET /api/session` |
| CLI | `Authorization: Bearer bzb_...` write token from `POST /api/auth/token` | nothing else |
| Website build | `Authorization: Bearer bzb_...` export token created in Settings (a build token in the panel) | can only call `GET /api/export.tar.gz`; `GET /api/backup.tar.gz` answers 403 |

Sessions last 12 hours. Each username gets ten sign-in attempts per 15 minutes;
a successful sign-in resets the count. When many sign-ins arrive at once and no
password-checking slot frees within two seconds, or one client address already
has four sign-ins in progress, the request is answered `503` `busy` with
`Retry-After: 5`; it is not counted against the username. There is no instance-wide limit, so one
attacker cannot lock every author out.

## Errors

Every error has the same shape. `message` says what to do; `detail` names the
specific cause when there is one.

```json
{ "error": { "code": "validation", "message": "Fix the highlighted field, then save again.", "detail": "title must not be empty." } }
```

Codes include `bad_request` (400: invalid JSON, an unknown `status`, a missing
`?version=`), `authentication` (401), `credentials` (401), `rate_limited`
(429), `origin`/`csrf`/`scope` (403), `not_found` (404), `validation` and
`preview_limit` (422), `busy` (503), `slug_taken`, `conflict`, `state` and
`deploy_unconfigured` (409), `version_required` (428), `too_large` (413) and
`content_type` (415).

A `conflict` also carries the post as it is now, so clients can show both
versions: `{ "error": {...}, "current": { ...post } }`.

## Sessions and tokens

| | |
|---|---|
| `GET /api/session` | `{authenticated, username, csrf}`; works without signing in. An export token is anonymous here (`authenticated: false`) |
| `POST /api/login` | `{username, password}` → session cookie and the same body as `GET /api/session` |
| `POST /api/logout` | ends the session |
| `POST /api/auth/token` | `{username, password, name}` → write token `{id, name, scope, secret}`; the secret is returned once |
| `GET /api/tokens` | every token (without secrets) |
| `POST /api/tokens` | `{name, scope: "write" or "export"}` → new token with its secret |
| `DELETE /api/tokens/{id}` | revoke |
| `POST /api/account/password` | `{current, next}`; signs out every other browser and revokes every write token (`burzblog login`); export tokens keep working |
| `GET /api/settings` | `{username, origin, version, deploy}` |
| `POST /api/deploy` | request a website rebuild now; 409 `deploy_unconfigured` when no deploy hook is set |

## Posts

A post:

```json
{
  "slug": "hello-world", "title": "Hello, World!", "description": "", "status": "draft",
  "body": "# Hi\n", "coverUrl": "", "coverAlt": "", "extra": "sitemap:\n  priority: 1\n",
  "publishedOn": null, "updatedOn": null, "publishAt": null,
  "createdAt": "...", "updatedAt": "...", "updatedBy": "ada", "version": 3
}
```

`extra` is YAML for frontmatter keys BurzBlog does not model. `publishAt` is
the time a draft publishes itself (UTC, RFC 3339), or `null`; see
[scheduled publishing](#scheduled-publishing).

| | |
|---|---|
| `GET /api/posts?status=&q=&sort=` | `{items, counts}`; items have no body; without `status`, archived posts are left out; `status` is `draft`, `published`, `archived` or `scheduled` (drafts with a `publishAt`); `counts` has `draft`, `published`, `archived` and `scheduled`, and scheduled drafts are counted in `draft` too; `sort` is `updated` (default, newest first), `published` (newest first, unpublished last), `title` (A to Z, ignoring case) or `created` (newest first); any other value is a 422; items carry `publishAt` |
| `POST /api/posts` | `{title, slug?, ...}` → new draft; the slug is made from the title when omitted |
| `GET /api/posts/{slug}` | one post |
| `PUT /api/posts/{slug}` | `{version, slug, title, description, body, coverUrl, coverAlt, extra, publishedOn, updatedOn}`; `version` must be current, a new `slug` renames |
| `POST /api/posts/{slug}/publish` | `{version}`; also `unpublish`, `archive`, `restore` |
| `POST /api/posts/{slug}/schedule` | `{version, at}` → the draft with `publishAt` set; see below |
| `POST /api/posts/{slug}/unschedule` | `{version}` → the draft without a `publishAt`; a no-op if it had none |
| `DELETE /api/posts/{slug}?version=N` | only archived posts; removes its history too |
| `GET /api/posts/{slug}/revisions` | `{items, current}`: earlier versions, newest first, without bodies; see below |
| `GET /api/posts/{slug}/revisions/{v}` | one earlier version in full |
| `POST /api/posts/{slug}/revisions/{v}/restore` | `{version}` → saves that content as a new version |
| `POST /api/preview` | `{markdown}` → `{html}`, sanitized; see [preview limits](#preview-limits) |

Each revision in the list carries `version`, `savedAt`, `savedBy` and a state:
`slug`, `title`, `status`, `words` (the body's word count) and the digests
`descriptionDigest`, `coverDigest` and `settingsDigest` (extra and dates).
`current` has the same state for the post as it is now. Equal digests mean the
field is equal, so the panel can say what a save changed by comparing a
revision with the next one, or with `current` for the newest. Digests are
opaque and may change between releases.

### Preview limits

A preview accepts a body of any size a post may have (2 MiB), but refuses text
built to make rendering explode. Saving is never affected. The refusal is a
422 `preview_limit` whose `detail` names the line to fix, which the panel shows
as "Preview is out of date: ...":

- more than 64 table columns, or more than 50,000 table cells in the post;
- a line that opens more than 32 block quotes or list items, or deeply nested
  lines adding up past 50,000 levels in total;
- more than 50,000 non-blank lines outside code blocks, or more than 1,000
  headings;
- a paragraph with more than 4,096 emphasis, code or link markers (`*`, `_`,
  `~`, `` ` ``, `[`), or long paragraphs whose squared marker counts add up past
  2^25.

A preview is also refused when the HTML would pass 8 MiB (422 `preview_limit`).
At most two previews render at once; one that waits more than 5 seconds for a
slot gets a 503 `busy` with `Retry-After: 5`, and the panel retries on the next
edit.

A save that changes nothing returns the post unchanged, without a new version.
Every change keeps the previous content as a revision.

### Scheduled publishing

`at` is an RFC 3339 time. It must be in the future and at most two years away,
and only drafts can be scheduled; anything else is a 422. The post stays a
draft, hidden from the export, until the time comes. Then the server publishes
it through the same versioned path as a manual publish: the content as it is
at that moment (edits made after scheduling are published too), a new version
with the previous one kept in history, `updatedBy` set to the account that
scheduled it, `publishedOn` set to the scheduled time unless the post already
had one, and the deploy hook called when one is configured. If the server was
down at the time, the post publishes when it starts. The scheduler checks at
least once a minute, so a post publishes within about a minute of its time.

Publishing, unpublishing or archiving a post by hand ends its schedule, as does
`unschedule`. Saving a post's text, or pushing a file with `status: draft`,
keeps the schedule. Scheduling and unscheduling are saves: each makes a new
version and needs the current `version`.

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
