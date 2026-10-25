# Security model

BurzBlog is a private writing tool for a few trusted authors. Readers never talk
to it; they read the website your build produces.

## What is protected, and how

**Accounts.** Passwords are hashed with Argon2id (19 MiB, 2 passes), at least
12 characters. At most two hashes run at once, so a burst of sign-ins cannot
exhaust a Pi's memory. A sign-in that cannot get a slot within two seconds is
answered `503` with `Retry-After` instead of queueing, so a flood of junk
usernames cannot build an unbounded queue, and one client address may have only
two sign-ins waiting. That per-address cap uses the connecting address only
(`X-Forwarded-For` is ignored on purpose), so it separates clients only when
they connect directly; behind a reverse proxy everyone shares the proxy's
address and a flood is shed as a whole. It is load shedding, never a lockout:
real sign-ins work again as soon as the flood stops. Unknown usernames take as
long as known ones, and nothing is shed by whether an account exists. Failed
sign-ins are limited per username, counted before the password is checked so
parallel guesses cannot slip past (see [API](api.md)); expired counters are
pruned as sign-ins arrive. There
is no self sign-up; accounts are created on the host with `burzblog user add`.

**Browser sessions.** A random 256-bit session id in an `HttpOnly`,
`SameSite=Strict` cookie, `Secure` and `__Host-` prefixed over HTTPS. Only its
SHA-256 digest is stored. Every change also needs the session's CSRF token in a
header and an `Origin` header equal to the configured origin; sign-in requires
the origin too, so another site cannot sign a browser into a different account.
Changing a password ends every other session.

**API tokens.** `bzb_` plus 256 random bits, shown once, stored as a SHA-256
digest. Write tokens (from `burzblog login`) can do what a signed-in user can.
Export tokens can only download published posts; the full backup
(`/api/backup.tar.gz`, drafts and archived posts included) needs a session or a
write token. Both are listed in Settings
with their last use and can be revoked there.

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

**Input.** Request bodies are capped at 4 MiB and JSON rejects unknown fields.
Frontmatter must be a plain mapping: YAML anchors, aliases and custom tags are
refused. Slugs are limited to lowercase letters, digits and hyphens, so they
are always safe file names in exports.

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
