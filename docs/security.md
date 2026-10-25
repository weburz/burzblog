# Security model

BurzBlog is a private writing tool for a few trusted authors. Readers never talk
to it; they read the website your build produces.

## What is protected, and how

**Accounts.** Passwords are hashed with Argon2id (19 MiB, 2 passes), at least
12 characters. At most two hashes run at once, so a burst of sign-ins cannot
exhaust a Pi's memory. A sign-in that cannot get a slot within two seconds is
answered `503` with `Retry-After` instead of queueing, so a flood of junk
usernames cannot build an unbounded queue, and one client address may have at
most four sign-ins in progress. That per-address cap uses the connecting address only
(`X-Forwarded-For` is ignored on purpose), so it separates clients only when
they connect directly; behind a reverse proxy everyone shares the proxy's
address and a flood is shed as a whole. It is load shedding, never a lockout:
real sign-ins work again as soon as the flood stops. Unknown usernames take as
long as known ones, and nothing is shed by whether an account exists. Failed
sign-ins are limited per username, counted before the password is checked so
parallel guesses cannot slip past (see [API](api.md)); expired counters are
pruned as sign-ins arrive. There
is no self sign-up; accounts are created on the host with `burzblog user add`,
or once, in the browser, by first-run setup (below).

**First-run setup.** `POST /api/setup` is the one unauthenticated write, and
it exists so a fresh install does not need a shell to get its first account.
It is not a content route and is gated three ways: it only works while the
store has zero accounts; it needs a one-time token that `burzblog serve` prints
in its own log and nowhere else; and it needs the same `Origin` as sign-in. The
token is 256 random bits (`crypto/rand`, base64url), held only as a SHA-256
digest in memory (a restart forgets it and prints a new one), valid for one
hour, compared in constant time and consumed by the account it creates. It dies
as soon as any account exists, including one made with `burzblog user add`
while the server runs. The link carries it in the URL fragment, so it never
reaches a proxy, an access log or a `Referer` header, and the panel removes it
from the address bar at once. The account is created in a transaction that
re-checks for zero accounts, so two concurrent requests with a valid token make
exactly one account (the other gets 409). Hashing uses sign-in's two slots and
per-address cap, and wrong tokens count against a per-address limit of ten per
15 minutes. A wrong, expired or used token gets the same 403. `GET /api/setup`
answers `{"needed": true|false}` without sign-in; that reveals only whether any
account exists, which the sign-in page's behavior would show anyway. Anyone who
can read the service log (`journalctl`, `docker logs`) while setup is open can
create the first account, so treat the log as sensitive until you have.

**Browser sessions.** A random 256-bit session id in an `HttpOnly`,
`SameSite=Strict` cookie, `Secure` and `__Host-` prefixed over HTTPS. Only its
SHA-256 digest is stored. Every change also needs the session's CSRF token in a
header and an `Origin` header equal to the configured origin; sign-in requires
the origin too, so another site cannot sign a browser into a different account.
Changing a password (in Settings or with `burzblog user passwd`) ends every other
session and revokes the account's write tokens in the same transaction, so a
credential minted by someone who knew the old password does not outlive the reset.

**API tokens.** `bzb_` plus 256 random bits, shown once, stored as a SHA-256
digest. Write tokens (from `burzblog login`) can do what a signed-in user can.
Export tokens can only read published content (the export and the
`GET /api/content/*` routes, which show the same data); the full backup
(`/api/backup.tar.gz`, drafts and archived posts included) needs a session or a
write token. Both are listed in Settings
with their last use and can be revoked there.

Export tokens are kept when a password changes: they cannot write, and revoking
them would silently break the website build. Revoke one in Settings if it leaks.
Outside the export and the content API an export token is refused with 403
`scope`, and `GET /api/session` treats it as anonymous, so it cannot read the
username.

**Content API.** `GET /api/content/*` serves published posts as JSON, Markdown
and sanitized HTML to a website. It needs a session or any token, so it is not
an unauthenticated content route, and it adds no data the export does not
already give an export token. Old slugs redirect (308) and are listed only while
their post is published, so a redirect is not a way to find unpublished posts. A draft, scheduled or archived post answers
exactly like a missing slug, so the API cannot be used to learn which exist.
The feeds and sitemap (`feed.xml`, `feed.atom`, `feed.json`, `sitemap.xml`) are
part of it and of the export: the same published data behind the same gate,
with links built from the operator's `--site-url`, never from request headers.
Its responses are `Cache-Control: private, max-age=60` with an `ETag` instead
of `no-store`: the website fetches the same posts often, and revalidating with
`If-None-Match` keeps that cheap on a small host. `private` keeps shared caches
and proxies from storing a response meant for one authorized caller, `Vary:
Authorization, Cookie` keys it to the credential, and the worst case is a post
shown up to a minute after an edit or an unpublish. Errors (including the 404s)
stay `no-store`, and the other security headers and the `default-src 'none'`
policy still apply. The HTML route renders through the preview path, so it has
the same input limits and render slots. CORS is not enabled: a website calls
the API from its server or build, not from a visitor's browser.

**The panel.** Served with a Content Security Policy that allows only the
panel's own scripts (inline bootstrap scripts by hash), no framing, no
`<base>` and no plugins; plus `nosniff`, `no-referrer`, a restrictive
Permissions-Policy, and HSTS over HTTPS. API responses are `no-store` and
`default-src 'none'`.

**Previews.** Markdown is rendered without raw HTML and then sanitized with an
allowlist (no scripts, event handlers, styles or `javascript:` URLs). The
result is shown in a sandboxed iframe that cannot run scripts at all, with its
own policy. Images in previews load from their `https://` URLs; that is the
author's own content.

**Preview cost.** Rendering is the one request whose cost depends on what is in
it, and any session or write token can ask for it every 250 ms. So
`POST /api/preview` is bounded in four ways. Before parsing, one linear pass
refuses constructs whose cost grows faster than their size (very wide or
numerous tables, deep nesting, huge numbers of lines or headings, paragraphs
with thousands of unmatched emphasis, code or link markers); the limits are in
[the API reference](api.md#preview-limits). Output is cut off at 8 MiB. Only
two renders run at once and a third waits at most 5 seconds before getting a
503. The body is still allowed up to the size of a post (2 MiB), so an author
can preview anything they can save. The largest input these limits admit takes
well under a second and allocates under 160 MiB in total (see
`internal/render/limits_test.go`); a typical 2 MiB post takes about 0.15 s.

**Input.** Request bodies are capped at 4 MiB and JSON rejects unknown fields.
Frontmatter must be a plain mapping: YAML anchors, aliases, merge keys (`<<`,
`!!merge`) and any tag outside the YAML core schema (`!!str`, `!!int`,
`!!float`, `!!bool`, `!!null`, `!!timestamp`, `!!map`, `!!seq`, `!!binary`) are
refused at every nesting level, so exports parse the same way in every YAML
reader. Extra frontmatter saved before these checks that still holds such
syntax is left out of exports (the rest of the post is exported) until it is
edited. Slugs are limited to lowercase letters, digits and hyphens, so they
are always safe file names in exports. Titles, descriptions, cover URLs and
alt text, token names and usernames are single-line text: they may not contain
control characters (U+0000 to U+001F, U+007F, U+0080 to U+009F), tabs included,
because terminals act on them. The body keeps tabs and line breaks and only
refuses NUL, so code samples with escape sequences still save. Existing data
is never rewritten: a post that already has such a character still loads, and
saving it again asks you to fix the named field.

**Terminal client.** `burzblog` never follows an HTTP redirect, because it
could carry your password or token to plain `http://` or to another host; it
stops and prints the address to run `burzblog login` with instead. Everything
the server sends that is printed (titles, names, error messages) has control
characters replaced with U+FFFD, and `pull -o -` does the same when writing to
a terminal.

**Scheduled posts.** A scheduled draft publishes as it is at the due time,
including edits made after scheduling, and still publishes if the session or
token that scheduled it was revoked: revoking access does not cancel a
schedule. Unschedule the post to stop it.

**Data at rest.** One SQLite file, created `0600` in a `0700` directory. Revisions
keep every earlier version of every post; they are only removed when an archived
post is deleted on purpose.

**Deploy hook.** The URL is secret: it is read from the environment, never
returned by the API, and left out of error messages.

## What is out of scope

- **Roles.** Every account can do everything, including revoking others'
  tokens. Only give accounts to people you trust with all posts.
- **TLS.** Terminate HTTPS in a reverse proxy (see [self-hosting](self-hosting.md)).
  BurzBlog ignores `X-Forwarded-*` headers.
- **Untrusted authors.** Authors control their post content, including image
  URLs in previews.

## Reporting a vulnerability

Email the maintainers listed in the repository rather than opening a public
issue.
