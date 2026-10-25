# HTTP API

Everything the panel, the CLI and the website do goes through this JSON API
under `/api`. There are no unauthenticated content routes.

## Authentication

| Caller | Credential | Changes need |
|---|---|---|
| Browser panel | `HttpOnly`, `SameSite=Strict` session cookie from `POST /api/login` (`__Host-burzblog` over HTTPS, `burzblog_session` over plain HTTP) | `Origin` equal to the configured origin, and `X-CSRF-Token` from `GET /api/session` |
| CLI | `Authorization: Bearer bzb_...` write token from `POST /api/auth/token` | nothing else |
| Website build | `Authorization: Bearer bzb_...` export token created in Settings (a build token in the panel) | can only read published content: `GET /api/export.tar.gz` and `GET /api/content/*`; every other route, `GET /api/backup.tar.gz` included, answers 403 `scope` |

Sessions last 12 hours. Each username gets ten sign-in attempts per 15 minutes;
a successful sign-in resets the count. A wrong two-factor code counts as a
failed attempt for the same username, and an account with two-factor sign-in
resets the count only when the code is right, not when the password is. When many sign-ins arrive at once and no
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
`?version=`), `authentication` (401), `credentials` (401), `code`,
`challenge` and `second_factor_required` (401: a wrong two-factor code, a missing
or expired sign-in challenge, a token request without its code), `rate_limited`
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
| `POST /api/login` | `{username, password}` → session cookie and the same body as `GET /api/session`. When the account uses two-factor sign-in the answer is `200 {"second_factor": true, "challenge": "..."}` and **no** cookie is set |
| `POST /api/login/totp` | `{challenge, code}` → session cookie and the `GET /api/session` body. `code` is the 6-digit code or a recovery code. The challenge lasts five minutes and works once; a wrong code answers 401 `code` (the challenge stays usable), an unknown, used or expired challenge 401 `challenge`. Needs the same `Origin` as `POST /api/login` |
| `POST /api/logout` | ends the session |
| `POST /api/auth/token` | `{username, password, name, code?}` → write token `{id, name, scope, secret}`; the secret is returned once. For an account with two-factor sign-in, `code` (a code or a recovery code) is required: without it the answer is 401 `second_factor_required` |
| `GET /api/tokens` | every token (without secrets) |
| `POST /api/tokens` | `{name, scope: "write" or "export"}` → new token with its secret |
| `DELETE /api/tokens/{id}` | revoke |
| `POST /api/account/password` | `{current, next}`; signs out every other browser and revokes every write token (`burzblog login`); export tokens keep working |
| `GET /api/account/2fa` | `{enabled, pending, recoveryCodes}`; `recoveryCodes` counts the unused ones |
| `POST /api/account/2fa/start` | `{password}` → `{secret, uri}`, a pending 160-bit secret (base32) and its `otpauth://` address; replaces an earlier pending secret; 409 `two_factor_on` if it is already on |
| `POST /api/account/2fa/confirm` | `{code}` → `{enabled: true, recoveryCodes: [10 codes]}`; the codes are shown only in this answer; 400 `code` if the code is wrong |
| `POST /api/account/2fa/disable` | `{password, code}`; ends every other browser session; 409 `two_factor_off` if it is not on |
| `GET /api/settings` | `{username, origin, version, deploy}` |
| `POST /api/deploy` | request a website rebuild now; 409 `deploy_unconfigured` when no deploy hook is set |

## Posts

A post:

```json
{
  "slug": "hello-world", "title": "Hello, World!", "description": "", "status": "draft",
  "body": "# Hi\n", "coverUrl": "", "coverAlt": "", "extra": "sitemap:\n  priority: 1\n",
  "tags": ["intro", "web-dev"], "author": "ada", "seoTitle": "", "seoDescription": "", "canonicalUrl": "",
  "publishedOn": null, "updatedOn": null, "publishAt": null,
  "createdAt": "...", "updatedAt": "...", "updatedBy": "ada", "version": 3
}
```

`extra` is YAML for frontmatter keys BurzBlog does not model. `publishAt` is
the time a draft publishes itself (UTC, RFC 3339), or `null`; see
[scheduled publishing](#scheduled-publishing).

Blog metadata, all optional:

| Field | Rules |
|---|---|
| `tags` | topics and categories in one list. Each tag is trimmed and lowercased, runs of anything but letters and digits become one hyphen (`Machine Learning` is saved as `machine-learning`), empty and repeated tags are dropped, and the order is kept. At most 20 tags of 40 characters each, otherwise a 422 `validation` |
| `author` | display name, one line, at most 100 characters. A new post's author is the account that created it unless the request names one |
| `seoTitle`, `seoDescription` | one line each, at most 120 and 320 characters; empty means the site uses the title and description |
| `canonicalUrl` | an absolute `https://` address, or empty |

Single-line fields reject control characters, tabs included, like `title`.

| | |
|---|---|
| `GET /api/posts?status=&q=&tag=&sort=&limit=&offset=` | `{items, total, counts}`; items have no body, but carry `tags` and `author`; `tag` keeps only posts with that tag (normalized like a post's tags, so `Machine Learning` finds `machine-learning`) and combines with the other filters, sorting and paging; `limit` (1 to 500) and `offset` page the result, and without `limit` every match is returned; `total` is the number of matches before paging, `counts` ignores the filter and paging; a bad `limit` or `offset` is a 422; without `status`, archived posts are left out; `status` is `draft`, `published`, `archived` or `scheduled` (drafts with a `publishAt`); `counts` has `draft`, `published`, `archived` and `scheduled`, and scheduled drafts are counted in `draft` too; `sort` is `updated` (default, newest first), `published` (newest first, unpublished last), `title` (A to Z, ignoring case) or `created` (newest first); any other value is a 422; items carry `publishAt` |
| `POST /api/posts` | `{title, slug?, ...}` → new draft; the slug is made from the title when omitted |
| `GET /api/posts/{slug}` | one post |
| `PUT /api/posts/{slug}` | `{version, slug, title, description, body, coverUrl, coverAlt, extra, tags, author, seoTitle, seoDescription, canonicalUrl, publishedOn, updatedOn}`; `version` must be current, a new `slug` renames. Like the other fields, a missing `tags` or `author` saves as empty |
| `GET /api/tags` | `{items: [{tag, count}]}`: every tag in use by any post, archived ones included, with its number of posts, by tag |
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
`descriptionDigest`, `coverDigest` and `settingsDigest` (extra, dates, tags,
author and SEO fields).
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
[publishing](publishing.md#the-file-format) for the format). `tags`, `author`,
`seo` (`title` and `description`) and `canonical` in the frontmatter are the
post's metadata fields above; other keys under `seo` stay in `extra`.

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

## Content API

Read-only access to published posts, for a website that fetches posts when it
builds or runs. Export tokens, write tokens and sessions may call it; it shows
the same data as the export. Drafts, scheduled and archived posts answer `404
not_found` exactly like a slug that does not exist, so a caller cannot tell
whether one exists. Without a credential every route answers 401.

| | |
|---|---|
| `GET /api/content/posts?q=&tag=&limit=&offset=` | `{items, total}`: published posts, newest published first (posts without a `publishedOn` last); `limit` (1 to 500), `offset`, `total` and their errors work as in `GET /api/posts`; `q` matches title, slug or description, case-insensitively; `tag` keeps posts with that tag, normalized like `GET /api/posts?tag=` |
| `GET /api/content/posts/{slug}` | one published post as JSON, with `body` |
| `GET /api/content/posts/{slug}?format=markdown` | `text/markdown`: the post's frontmatter document, byte for byte what `export.tar.gz` holds as `<slug>.md`; `format` is `json` (default) or `markdown`, anything else is a 422 |
| `GET /api/content/posts/{slug}/html` | `{slug, version, html}`: the body rendered and sanitized like `POST /api/preview`, with the same [limits](#preview-limits) (422 `preview_limit`, 503 `busy`) |
| `GET /api/content/tags` | `{items: [{tag, count}]}`: tags of published posts by name, with how many published posts carry each; tags used only by drafts, scheduled or archived posts are left out |

An item is `{slug, title, description, coverUrl, coverAlt, tags, author,
seoTitle, seoDescription, canonicalUrl, publishedOn, updatedOn, updatedAt,
words, readingMinutes, version}`; `tags` is always a list. The single post adds
`body` (Markdown, without frontmatter). `readingMinutes` is `words` at 200 a
minute, rounded up. Status, the accounts that saved it and `extra` are not part
of it; `extra` is in the `format=markdown` document.

Responses carry `ETag` and `Cache-Control: private, max-age=60`, and
`If-None-Match` (a tag, a list of tags or `*`) answers `304` without a body.
The tag of a post is its `version` (`"3"`, `"3-md"` for Markdown, `"3-html"`
for HTML); the tag of the list and of the tags is a digest of the response. Publishing,
unpublishing or editing a post changes them, so a cached copy is at most 60
seconds old.

## Health

`GET /healthz` → `{"status": "ok", "version": "..."}` when the database answers.
