# HTTP API

Everything the panel, the CLI and the website do goes through this JSON API
under `/api`. There are no unauthenticated content routes. The one public
route outside `/api` serves uploaded files at `/media/{name}`; see
[Media](#media).

## Authentication

| Caller | Credential | Changes need |
|---|---|---|
| Browser panel | `HttpOnly`, `SameSite=Strict` session cookie from `POST /api/login` (`__Host-burzblog` over HTTPS, `burzblog_session` over plain HTTP) | `Origin` equal to the configured origin, and `X-CSRF-Token` from `GET /api/session` |
| CLI | `Authorization: Bearer bzb_...` write token from `POST /api/auth/token` | nothing else |
| Website build | `Authorization: Bearer bzb_...` export token created in Settings (a build token in the panel) | can only read published content: `GET /api/export.tar.gz` and `GET /api/content/*`; every other route, `GET /api/backup.tar.gz` included, answers 403 `scope` |

Every account is an `admin` or an `author`, and a token has the role of its
owner. Routes only admins may use answer an author 403 `role` (see the matrix in
[security](security.md)): deleting a post, other accounts' tokens, export tokens,
`/api/settings`, `/api/deploy`, `/api/audit` and both downloads. An export token
owned by an author reads the content API but gets 403 `role` on
`GET /api/export.tar.gz`; make the build's export token with an admin account.

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
(429), `origin`/`csrf`/`scope`/`role`/`setup_token` (403), `not_found` (404), `validation` and
`preview_limit` (422), `busy` (503), `slug_taken`, `conflict`, `state`, `setup_done` and
`deploy_unconfigured` and `in_use` (409), `version_required` (428), `too_large`
(413), `content_type` and `unsupported_type` (415) and `media_disabled` (501).

A `conflict` also carries the post as it is now, so clients can show both
versions: `{ "error": {...}, "current": { ...post } }`.

## Sessions and tokens

| | |
|---|---|
| `GET /api/session` | `{authenticated, username, role, csrf}` (`role` is `admin` or `author`); works without signing in. An export token is anonymous here (`authenticated: false`) |
| `GET /api/setup` | `{needed}`: true while the server has no accounts. Needs no sign-in and reveals nothing else |
| `POST /api/setup` | `{token, username, password}` creates the first account and signs the browser in (201, same body as `GET /api/session`, plus the session cookie). Needs the `Origin` header. `token` is the one-time value after `#token=` in the link `burzblog serve` prints. 403 `setup_token` for a wrong, expired or used token (one message for all), 409 `setup_done` once any account exists, 400 `validation` for a name or password `burzblog user add` would refuse, 429 `rate_limited` and 503 `busy` like sign-in |
| `POST /api/login` | `{username, password}` → session cookie and the same body as `GET /api/session`. When the account uses two-factor sign-in the answer is `200 {"second_factor": true, "challenge": "..."}` and **no** cookie is set |
| `POST /api/login/totp` | `{challenge, code}` → session cookie and the `GET /api/session` body. `code` is the 6-digit code or a recovery code. The challenge lasts five minutes and works once; a wrong code answers 401 `code` (the challenge stays usable), an unknown, used or expired challenge 401 `challenge`. Needs the same `Origin` as `POST /api/login` |
| `POST /api/logout` | ends the session |
| `POST /api/auth/token` | `{username, password, name, code?}` → write token `{id, name, scope, secret}`; the secret is returned once. For an account with two-factor sign-in, `code` (a code or a recovery code) is required: without it the answer is 401 `second_factor_required` |
| `GET /api/tokens` | tokens without secrets: every account's for an admin, only your own for an author |
| `POST /api/tokens` | `{name, scope: "write" or "export"}` → new token with its secret; 403 `role` for an author asking for `export` |
| `DELETE /api/tokens/{id}` | revoke; an author may revoke only their own (403 `role` otherwise) |
| `POST /api/account/password` | `{current, next}`; signs out every other browser and revokes every write token (`burzblog login`); export tokens keep working |
| `GET /api/account/2fa` | `{enabled, pending, recoveryCodes}`; `recoveryCodes` counts the unused ones |
| `POST /api/account/2fa/start` | `{password}` → `{secret, uri}`, a pending 160-bit secret (base32) and its `otpauth://` address; replaces an earlier pending secret; 409 `two_factor_on` if it is already on |
| `POST /api/account/2fa/confirm` | `{code}` → `{enabled: true, recoveryCodes: [10 codes]}`; the codes are shown only in this answer; 400 `code` if the code is wrong |
| `POST /api/account/2fa/disable` | `{password, code}`; ends every other browser session; 409 `two_factor_off` if it is not on |
| `GET /api/settings` | admins only. `{username, origin, version, deploy, site}`; `site` is `{configured, feeds}` plus, when a site URL is set, `{url, title, description, language, postPath, tagPath}` (`feeds` lists the feed and sitemap routes, empty when not configured) |
| `POST /api/deploy` | admins only. Request a website rebuild now; 409 `deploy_unconfigured` when no deploy hook is set |
| `GET /api/audit` | admins only. `?limit=` (1 to 500, default 100) and `?offset=` page the audit log, newest first: `{items: [{id, at, actorId?, actor, tokenId?, action, target, remote}], total}`. `actor` is an account name, or `host` or `scheduler`; `actorId` and `tokenId` are left out when there is none. Actions: `login.success`, `login.failure`, `account.create`, `role.change`, `password.change`, `2fa.enable`, `2fa.disable`, `2fa.recovery`, `token.create`, `token.revoke`, `post.publish`, `post.unpublish`, `post.archive`, `post.restore`, `post.schedule`, `post.unschedule`, `post.delete`, `redirect.delete`, `deploy`, `backup.download`. There is no route to change or remove an entry |

## Posts

A post:

```json
{
  "slug": "hello-world", "title": "Hello, World!", "description": "", "status": "draft",
  "body": "# Hi\n", "coverUrl": "", "coverAlt": "", "extra": "sitemap:\n  priority: 1\n",
  "tags": ["intro", "web-dev"], "author": "ada", "seoTitle": "", "seoDescription": "", "canonicalUrl": "",
  "featured": false, "publishedOn": null, "updatedOn": null, "publishAt": null,
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
| `featured` | `true` marks a post a site should lead with, `false` (the default) does not. Saving it is a change like any other: it makes a new version and the previous one is kept as a revision. A `PUT` that leaves `featured` out keeps the post's current value (unlike `tags` and `author`), so clients written before the flag cannot unfeature a post by accident |

Single-line fields reject control characters, tabs included, like `title`.

| | |
|---|---|
| `GET /api/posts?status=&q=&tag=&featured=&sort=&limit=&offset=` | `{items, total, counts}`; items have no body, but carry `tags`, `author` and `featured`; `featured` is `true` or `false` and keeps only featured or only other posts (any other value is a 422 `validation`); `q` searches title, description, body, tags, author and slug (see below); `tag` keeps only posts with that tag (normalized like a post's tags, so `Machine Learning` finds `machine-learning`) and combines with the other filters, sorting and paging; `limit` (1 to 500) and `offset` page the result, and without `limit` every match is returned; `total` is the number of matches before paging, `counts` ignores the filter and paging; a bad `limit` or `offset` is a 422; without `status`, archived posts are left out; `status` is `draft`, `published`, `archived` or `scheduled` (drafts with a `publishAt`); `counts` has `draft`, `published`, `archived` and `scheduled`, and scheduled drafts are counted in `draft` too; `sort` is `updated` (default, newest first), `published` (newest first, unpublished last), `title` (A to Z, ignoring case) or `created` (newest first); any other value is a 422; items carry `publishAt` |
| `POST /api/posts` | `{title, slug?, ...}` → new draft; the slug is made from the title when omitted |
| `GET /api/posts/{slug}` | one post |
| `PUT /api/posts/{slug}` | `{version, slug, title, description, body, coverUrl, coverAlt, extra, tags, author, seoTitle, seoDescription, canonicalUrl, featured, publishedOn, updatedOn}`; `version` must be current, a new `slug` renames. Like the other fields, a missing `tags` or `author` saves as empty |
| `GET /api/tags` | `{items: [{tag, count, label, custom}]}`: every tag in use by any post, archived ones included, with its number of posts and the label shown for it, by tag; `custom` is `true` when an admin set the label and `false` when it is [derived](#tag-labels) |
| `PUT /api/tags/{tag}/label` | admins only (403 `role`): `{label}` → `{tag, label, custom: true}`; the tag must already be normalized (422 `validation` otherwise) and used by some post (404 `not_found`); see [tag labels](#tag-labels) |
| `DELETE /api/tags/{tag}/label` | admins only (403 `role`): removes the label so the derived one shows again → `{tag, label, custom: false}`; succeeds when the tag had no label |
| `POST /api/posts/{slug}/publish` | `{version}`; also `unpublish`, `archive`, `restore` |
| `POST /api/posts/{slug}/schedule` | `{version, at}` → the draft with `publishAt` set; see below |
| `POST /api/posts/{slug}/unschedule` | `{version}` → the draft without a `publishAt`; a no-op if it had none |
| `DELETE /api/posts/{slug}?version=N` | admins only (403 `role`). Only archived posts; removes its history and its redirects too |
| `GET /api/posts/{slug}/redirects` | `{items: ["old-slug", ...]}`: the old slugs that redirect to the post, by slug, whatever the post's status; see [slug redirects](#slug-redirects) |
| `DELETE /api/posts/{slug}/redirects/{from}` | removes the redirect from one old slug of this post → `{deleted: true}`; 404 `not_found` if the post has no such redirect |
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

### Search

`q` on `GET /api/posts` and `GET /api/content/posts` searches the title,
description, body, tags, author and slug of posts, ignoring case and accents.
What you type is read as plain words: every word must appear and the last one
matches as a prefix, so `gol` finds `golang`. Quotes, `*`, `-`, `OR`, `NEAR`,
`title:` and the like are not search syntax; punctuation separates words and
`OR` is just a word. At most 16 words count. A `q` without any word filters
nothing. Without `sort`, results come best match first (title matches weigh
most, then description, tags, slug, author and body); with `sort` the sort wins.
`total` and paging work as without `q`. In the content API each item of a
search also has `snippet`, up to about 24 words of the Markdown body near the
match, as plain text (Markdown syntax included, never HTML): escape it before
you put it in a page. Drafts, scheduled and archived posts are never in content
API results.

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

### Slug redirects

Renaming a post (a `PUT` with a new `slug`) keeps its old slug
as a redirect to the post, so addresses on your website do not break. The
redirect belongs to the post, not to a slug, so renaming again points every
earlier slug at the newest one (`a` to `b` to `c` leaves `a` and `b` both going
to `c`). A slug that is live always wins: creating a post or renaming one to a
slug that is only a redirect removes that redirect, and a post renamed back to
an old slug stops redirecting from it. Deleting a post deletes its redirects.
Restoring a revision keeps the current slug, so it adds none. Removing a
redirect with `DELETE` is the only other way to end one. Redirects are
published to websites only while their post is published; see the
[content API](#content-api).

## Media

Images and PDFs for posts. Uploading, listing and deleting need a session or a
write token, like every other change (`Origin` and `X-CSRF-Token` for the
browser); export tokens get 403 `scope`.

| | |
|---|---|
| `POST /api/media` | `multipart/form-data` with a `file` part and an optional `alt` text field (at most 1000 bytes). 201 with the record, or 200 with the existing record when the same bytes were uploaded before (the first upload's `alt` and name stay) |
| `GET /api/media` | `{items, total}`, newest first; `?limit=` (1 to 500) and `?offset=` page it, without `limit` every upload is returned |
| `DELETE /api/media/{id}` | admins only (403 `role`); deletes the record and the file. 409 `in_use` names up to ten posts (any status) whose body or cover mention the file; `?force=true` deletes anyway and leaves those links broken. Earlier revisions are not checked |
| `GET /media/{name}` | the file itself. **Public, no sign-in** (see [security](security.md#uploaded-files)) |

A record is `{id, name, url, originalName, mime, size, width, height, alt,
uploadedBy, createdAt}`. `url` is absolute, built from the configured origin
(`--origin`), so a post that embeds it keeps working in an export or on the
website. `width` and `height` are present for JPEG, PNG and GIF only; BurzBlog
does no image processing.

What is accepted is decided from the file's first bytes, never from the name or
the `Content-Type` the client sends: JPEG, PNG, GIF, WebP, AVIF and PDF. JPEG,
PNG and GIF must also decode. Anything else, SVG included, is 415
`unsupported_type`; a file that starts like a PNG, JPEG or GIF but does not
decode is 422 `validation`. The size limit is 10 MiB, set with
`burzblog serve --max-upload-mb` or `BURZBLOG_MAX_UPLOAD_MB` (up to 256); a
bigger file is 413 `too_large`. A server without media storage answers 501
`media_disabled`.

The stored `name` is 32 hexadecimal characters of the file's SHA-256 digest and
an extension from its detected type (`.jpg`, `.png`, `.gif`, `.webp`, `.avif`,
`.pdf`); the uploaded file name is kept only as `originalName`, for display.
The same bytes therefore always have the same address.

### Tag labels

A tag is its normalized slug (`constitutional-law`), which is its identity in
posts, filters and URLs. A tag also has a display label for pages and menus. A
tag nobody labelled shows the label derived from it: hyphens become spaces and
every word starts with a capital letter, so `constitutional-law` is
`Constitutional Law` and `go` is `Go`. An admin can set a label of their own with
`PUT /api/tags/{tag}/label`; `DELETE` returns the tag to the derived label.

A label is trimmed, one line, 1 to 60 characters, any Unicode text, without
control characters (422 `validation` otherwise). The label is kept by tag, not
by post: removing a tag from every post leaves its label stored, and it shows
again if the tag returns. Changing or clearing a label is recorded in the
[audit log](security.md) as `tag.label`, and triggers a website rebuild when a
deploy hook is set. Labels never rename a tag: `tags` on posts and `?tag=` stay
slugs.

## Markdown documents

What the CLI edits: the post as frontmatter plus body (see
[publishing](publishing.md#the-file-format) for the format). `tags`, `author`,
`seo` (`title` and `description`) and `canonical` in the frontmatter are the
post's metadata fields above (and `featured: true`, written only when the post
is featured); other keys under `seo` stay in `extra`.

| | |
|---|---|
| `GET /api/posts/{slug}/markdown` | `text/markdown`, with `ETag: "<version>"` |
| `PUT /api/posts/{slug}/markdown` | `If-Match: "<version>"` saves; `If-None-Match: *` creates a new post |
| `GET /api/export.tar.gz` | every published post as `<slug>.md` in a gzipped tar, then `feed.xml`, `feed.atom`, `feed.json` and `sitemap.xml` when a [site URL](#feeds-and-sitemap) is set; admins only (403 `role`, also for an author's token); export tokens allowed |
| `GET /api/backup.tar.gz` | every post, drafts and archived included, as `<status>/<slug>.md`, a `README.txt`, and every uploaded file as `media/<name>`, in a gzipped tar; admins only (403 `role`); session or write token, not export tokens |

The backup holds current posts only, always with a `status` line (archived posts
are stored as `draft`; the folder says `archived`). Revisions are not included:
back up the database file for those (see [self-hosting](self-hosting.md#backups)).
Uploaded files are added as they are stored; one that is missing from the data
directory is logged and skipped. The export holds no files: posts refer to
uploads by their absolute `url`. The response is `Content-Disposition: attachment` with a dated name such as
`burzblog-backup-2026-10-25.tar.gz`, and `Cache-Control: no-store`.

## Content API

Read-only access to published posts, for a website that fetches posts when it
builds or runs. Export tokens, write tokens and sessions may call it; it shows
the same data as the export. Drafts, scheduled and archived posts answer `404
not_found` exactly like a slug that does not exist, so a caller cannot tell
whether one exists. Without a credential every route answers 401.

| | |
|---|---|
| `GET /api/content/posts?q=&tag=&featured=&include=&limit=&offset=` | `{items, total}`: published posts, newest published first (posts without a `publishedOn` last); `limit` (1 to 500), `offset`, `total` and their errors work as in `GET /api/posts`; `q` searches as in `GET /api/posts` (see below) and returns the best match first, with a plain-text `snippet` on each item; `tag` keeps posts with that tag, normalized like `GET /api/posts?tag=`; `featured=true` keeps only featured posts and `featured=false` only the others (any other value is a 422 `validation`), in the same newest-first order, and combines with `q`, `tag` and paging; `include=extra` adds each item's `extra` (see below), any other value is a 422 |
| `GET /api/content/posts/{slug}` | one published post as JSON, with `body` and `extra` |
| `GET /api/content/posts/{slug}?format=markdown` | `text/markdown`: the post's frontmatter document, byte for byte what `export.tar.gz` holds as `<slug>.md`; `format` is `json` (default) or `markdown`, anything else is a 422 |
| `GET /api/content/posts/{slug}/html` | `{slug, version, html}`: the body rendered and sanitized like `POST /api/preview`, with the same [limits](#preview-limits) (422 `preview_limit`, 503 `busy`) |
| `GET /api/content/redirects` | `{items: [{from, to}]}`: old slugs of renamed published posts by `from`, with the post's current slug as `to`; redirects to drafts, scheduled and archived posts are left out |
| `GET /api/content/tags` | `{items: [{tag, count, label}]}`: tags of published posts by name, with how many published posts carry each and its display label (see [tag labels](#tag-labels)); tags used only by drafts, scheduled or archived posts are left out |
| `GET /api/content/feed.xml` | RSS 2.0, `application/rss+xml`; see [Feeds and sitemap](#feeds-and-sitemap) |
| `GET /api/content/feed.atom` | Atom 1.0, `application/atom+xml` |
| `GET /api/content/feed.json` | JSON Feed 1.1, `application/feed+json` |
| `GET /api/content/sitemap.xml` | sitemap, `application/xml` |

An old slug of a published post, on `GET /api/content/posts/{slug}` and its
`/html` form, answers `308 Permanent Redirect` with `Location:
/api/content/posts/<current-slug>` (the rest of the path and the query are
kept) and the body `{"redirect": "<current-slug>"}`. Clients that follow
redirects end at the post. If the post is not published, or the slug was never
a redirect, the answer is the same `404 not_found` as for any missing slug, so a
redirect cannot be used to find unpublished posts. Redirects are not part of
`GET /api/content/posts` or its items.

An item is `{slug, title, description, coverUrl, coverAlt, coverWidth,
coverHeight, tags, tagLabels, featured, author, seoTitle, seoDescription,
canonicalUrl, publishedOn, updatedOn, updatedAt, words, readingMinutes,
version}`; `tags` is always a list of slugs, `tagLabels` maps each of them to
its display label (`{"constitutional-law": "Constitutional Law"}`, `{}` without
tags) and only ever holds the post's own tags, and `featured` is a boolean. The
single post adds `body` (Markdown, without frontmatter) and `extra`.
`readingMinutes` is `words` at 200 a minute, rounded up. Status and the
accounts that saved it are not part of it.

- `coverWidth` and `coverHeight` are the pixel size of the cover, set only when
  `coverUrl` points to an image uploaded to this server (an address under
  `/media/` on `--origin`) whose format BurzBlog decodes. For any other cover
  they are left out, not `null`, so a client treats absence as "unknown". They
  let a page reserve the image's space before it loads. Sizes come from the
  upload record in one query for the whole page, not one per item.
- `extra` holds the frontmatter keys BurzBlog does not model, the same keys the
  `format=markdown` document carries, as a JSON object (`{}` when there are none).
  It is on the single post always and on the list only with `?include=extra`,
  because it can be large. Values keep their YAML type: nested maps, lists,
  strings, numbers, booleans and `null`. Dates and timestamps are strings as
  written in the frontmatter (`2013-08-29`, not RFC 3339 added to them); map
  keys that are not strings are written as strings (`1` for `1:`); `.inf`,
  `-.inf` and `.nan` are the strings `"+Inf"`, `"-Inf"` and `"NaN"`. Extra
  that cannot be read, such as a file stored by an older release, is `{}` and
  the server logs a warning with the slug, so one bad post never fails a list.
  `extra` never holds anything but frontmatter.

Responses carry `ETag` and `Cache-Control: private, max-age=60`, and
`If-None-Match` (a tag, a list of tags or `*`) answers `304` without a body.
The tag of a post is its `version` (`"3"`, `"3-md"` for Markdown, `"3-html"`
for HTML), followed by a digest of the labels an admin set for its tags when
there are any and by the cover size when it has one (`"3-9f2c41a7b0de"`,
`"3-1200x630"`, `"3-9f2c41a7b0de-1200x630"`), so changing a label or the
cover's upload changes it; the tag of the list and of the tags is a digest of
the response. Publishing,
unpublishing or editing a post changes them, so a cached copy is at most 60
seconds old.

### Stability and versioning

The content API follows the semantic version of the BurzBlog release, so a
website can pin to a major version. Covered are every `/api/content/*` route,
its JSON shapes, its error shape (`{"error": {code, message, detail}}`) and its
authentication, and the format of `GET /api/export.tar.gz`. Within a major
version they change only additively: new fields, new optional query
parameters, new routes. Clients must ignore fields they do not know. Removing
or renaming a field, route or parameter, or changing what a field or status code
means, needs a new major version and a `CHANGELOG.md` entry. The other `/api/*`
routes belong to the browser panel and the terminal client shipped in the same
binary; they are internal and may change in any release.

### Feeds and sitemap

Readers never talk to BurzBlog, so feeds and the sitemap are made for the
website to serve. They sit behind the same credential as the rest of the content
API and are also in the export archive. They need the website's public address
for absolute links: start the server with `--site-url` (see
[self-hosting](self-hosting.md#settings)). Without it each route answers `409
site_url_missing` with the flag to set, and the export leaves the files out.

- The feeds hold the newest published posts: `?limit=` (1 to 100, default 20)
  and `?tag=` (normalized like the post list) choose which. Each item has the
  title, link, id, dates, description, author, tags as categories and the body
  as sanitized HTML (the same renderer and limits as `/html`). A post the
  renderer refuses (422 `preview_limit` there) or cannot render in time keeps its
  description only, so the feed still answers 200. Links in the body are written
  as the author wrote them; relative ones are not rewritten.
- Links and ids are `<site URL><post path>` with `{slug}` replaced, by default
  `https://example.com/blog/<slug>`, so they stay stable when the server moves.
  RSS items carry `guid isPermaLink="true"`, `pubDate` in RFC 822 form and the
  author in `dc:creator`; Atom and JSON Feed use RFC 3339 dates. A post's
  "updated" date is its `updatedOn`, else `publishedOn`, else the last save.
- The sitemap lists every published post with `lastmod` (the same date), then a
  page per tag at the tag path when `--tag-path` is set. A sitemap file may hold
  50,000 URLs; BurzBlog does not write an index, so beyond that the newest posts
  are listed and tag pages are dropped first, and the server logs a warning.
- Like the other content routes they answer with an `ETag`, `304` for
  `If-None-Match` and `Cache-Control: private, max-age=60`. The tag is a digest
  of the site settings, the request and each included post's slug and version,
  so a `304` costs no rendering.

## Health

`GET /healthz` → `{"status": "ok", "version": "..."}` when the database answers.
