# ADR-008: Single-project authenticated application

Status: accepted for the application milestone; browser UI is separate.

## Decision

Use FastAPI/Uvicorn for a typed HTTP boundary over application services and the
existing file/Git domains. Bind the operator command to loopback. Deploy behind
a same-origin HTTPS reverse proxy; do not enable CORS or accept browser-supplied
repository roots or remote endpoints. Publish changes portable frontmatter only.

SQLite now has a narrowly justified purpose: one Argon2id administrator hash,
hashed opaque session credentials, session-bound CSRF tokens and a persistent
login-attempt window. The private 0700 state directory must be outside content
and its Git repository. No article body, title or derived content index is stored
there. Setup uses a local terminal, not a remotely reachable registration flow.

Sessions expire after 12 hours and are revoked at logout. Cookies are HttpOnly,
SameSite=Strict and Secure for HTTPS origins. Every mutation checks exact Origin,
JSON content type and a separate session CSRF token (login checks Origin and JSON).
HTTP is accepted only for explicitly loopback development origins. A global ten
attempts/ten-minute window bounds expensive password verification; this can be
used to deny login temporarily, a documented small-instance tradeoff.

Preview uses markdown-it-py with raw HTML disabled followed by nh3 sanitization.
Images and embedded content are omitted to avoid background tracking requests.
No server fetches occur. The original source remains untouched.

## Consequences

The runtime gains maintained HTTP, password-hashing and sanitization dependencies,
but no separate database service, worker or queue. There is no password-reset
endpoint, multi-user authorization model, OAuth or public registration. Backups
now include private operational state separately from portable content.

Requests are bounded before JSON parsing. Errors omit private paths, credentials
and validation input. Repository errors stop operations; recovery instructions
remain operator-facing. The reverse proxy must enforce timeouts and connection
limits. This is not a hardened public multi-tenant hosting boundary.

No browser interface, browser E2E assurance, private remote credentials or image
publication is implied by this milestone.
