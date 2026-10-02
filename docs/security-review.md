# Focused product-boundary review — 2026-10-02

This is an implementation review and test record, not an independent penetration
test or production-readiness certification. Use a dedicated unprivileged OS user,
local POSIX storage, operator-controlled ancestors and an HTTPS reverse proxy.

## Filesystem and concurrency

Descriptor-relative no-follow traversal, regular-file ownership/group/mode and
single-link checks prevent supported-path escapes and accidental cross-file
modification. Deterministic tests cover traversal, symlinks, ownership, stale
revisions, source replacement, destination appearance and failures around
publication/moves. Recovery keeps actual source inodes and independent requested
bytes. Late external writes remain recoverable; journal pruning is never automatic.
API tests also inject an external replacement immediately after a save: the
response reports conflict instead of substituting external text as saved input.

Not guaranteed: hostile same-UID processes rearranging ancestor directories,
network filesystems, every power-loss/filesystem scenario, ACL/xattr preservation,
automatic reconciliation, or protection against someone deliberately deleting
the recovery directory. Recovery may require manual intervention with an absent
normal source path. Backups remain necessary.

## Git

Argument-array execution, bounded output/time, isolated configuration/environment,
disabled hooks/signing/helpers, explicit endpoints and content-path selection are
tested. Local bare receiver hooks are rejected. Branch/update changes use the
recovery-aware content store, not an overwriting checkout. Divergent histories
stop; force push/reset/clean/automatic merge/rebase are absent. Private credentials
and arbitrary SSH transports are not implemented. No browser can select a new
filesystem root or remote URL. Operator-selected HTTPS endpoints are a trusted
configuration boundary, not a public URL-fetch service.

Not guaranteed: externally concurrent Git metadata writers, linked worktrees,
arbitrary filters/configuration, non-Markdown branch transitions, real Internet
remote reliability or private repository authentication. Use ordinary Git for
unsupported operations rather than weakening the validation whitelist.

## HTTP and authentication

One out-of-band initialized Argon2id administrator; state outside content and Git;
private SQLite files; hashed opaque sessions; expiry and logout revocation;
HttpOnly/SameSite cookies; Secure cookies for HTTPS; exact-Origin plus CSRF-token
checks on mutations; bounded JSON bodies and content-free error responses.
Global persistent login throttling is complemented by one concurrent Argon2
verification per process. A deterministic concurrency test checks the bound.
Built-in development and production commands bind loopback.

Not guaranteed: distributed/multi-worker rate limits, protection from temporary
login denial, a password reset workflow, multi-user roles, every slow-client or
resource-exhaustion attack. The public proxy must impose timeouts and connection
limits. No external deployment or TLS termination was tested in this environment.

## Browser and preview

Markdown raw HTML is disabled and output sanitized. A sandboxed iframe adds a
deny-by-default preview CSP. Browser tests verify malicious content does not run
scripts/load images, authenticated API reads are protected, path escape fails,
conflicts preserve editor text, and offline failures never display saved status.
No draft/token localStorage, telemetry, analytics, external fonts or runtime icon
fetches. Source no-op CRLF preservation and dirty-buffer disk refresh have tests.

Not guaranteed: offline durability, recovery from closing an unsaved tab, visual
round-trip editing, a full accessibility audit, Safari/Firefox or physical mobile
device certification. Native source editing is intentional, not a claimed visual
editor. Browser acceptance currently runs Chromium at three viewport sizes.

## Supply chain and remaining work

Locks, explicit build approvals, pinned dormant Actions, read-only CI permissions
and no deployment/publication workflows. Actions remains disabled. Audit findings
are not suppressed: the available esbuild patch is applied; node-forge's upstream
development-TLS advisory remains tracked in #11, excluded from production output
by an enforced build check. The existing pytest Dependabot PR is unrelated and
was not merged automatically. See [dependency details](dependencies.md).

Docker runtime/web packaging remains #5. License/security/community contacts
remain #3. Private Git credential support needs a separate threat-modeled design.
