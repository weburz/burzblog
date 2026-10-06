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
stop; force push/reset/clean/automatic merge/rebase are absent. Private HTTPS
credentials (ADR-010) are operator file references sent only as a URL-scoped
header through the Git child environment; SSH transports are not implemented. No browser can select a new
filesystem root or remote URL. Operator-selected HTTPS endpoints are a trusted
configuration boundary, not a public URL-fetch service.

Not guaranteed: externally concurrent Git metadata writers, linked worktrees,
arbitrary filters/configuration, non-Markdown branch transitions, real Internet
remote reliability, SSH, proxies or secrets protected from the service OS user. Use ordinary Git for
unsupported operations rather than weakening the validation whitelist.

## HTTP and authentication

One out-of-band initialized Argon2id administrator; state outside content and Git;
private SQLite files; hashed opaque sessions; expiry and logout revocation;
HttpOnly/SameSite cookies; Secure cookies for HTTPS; exact-Origin plus CSRF-token
checks on mutations; bounded JSON bodies and content-free error responses.
Global persistent login throttling is complemented by one concurrent Argon2
verification per process. A deterministic concurrency test checks the bound.
Built-in development and production commands bind loopback.

Terminal-only `reset-admin` (#17) replaces the hash, revokes every session and
clears the attempt window in one transaction; it has no HTTP surface. Since
2026-10-03 the administrator commands validate only the storage boundary and
authentication state (`Settings.for_administration` + `validate_storage`), so
Git credentials or connectivity cannot block recovery; `serve` still validates
everything. Authentication state is also refused when its tables do not have
exactly the expected columns, and SQLite errors are reported without a traceback.
`reset-admin` refuses to create state in a missing or empty directory.

Not guaranteed: distributed/multi-worker rate limits, protection from temporary
login denial, a web/email password reset, multi-user roles, every slow-client or
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

Docker runtime/web packaging remains #5. License: Apache-2.0, decided 2026-10-06;
no separate reporting policy for v1 (see security.md).

## Private remote credentials — 2026-10-02 (#13)

Tests use a disposable TLS server running `git http-backend` with Basic
authentication. Covered: authenticated fetch/push/update; wrong and revoked
tokens (fixed message, remote unchanged); non-fast-forward over HTTPS; TLS
refusal without a trusted CA before any request is sent; redirects not
followed and credentials not forwarded; secret absent from argv (recorded
`Popen` arguments), `.git`, content, state, API bodies, CLI output, logs and
exception text; nine hostile repository settings (helpers, `http.*`,
`insteadOf`, upload-pack, askPass, sshCommand, include) refused before contact
with no command executed; ambient `~/.gitconfig`, `.git-credentials`, `.netrc`,
askpass and `GIT_CONFIG_*` variables ignored; malformed, oversized, binary,
symlinked, FIFO, directory, relative, group/world-readable and misplaced secret
files refused; browser payloads with URL/username/path fields rejected (422)
without contacting the remote; credential-free HTTPS and local bare remotes
unchanged. Browser E2E pushes and fetches through the same authenticated TLS
fixture and asserts the token never reaches the page.

Residual risks (credentials): the service OS user can read the secret and its Git children's
environment; an authenticated browser session can push committed content to the
configured remote; Git/libcurl are trusted native code. Not covered: real
provider endpoints, proxies, SSH, and Git older than 2.31 beyond a version check.

## Production topology and container — 2026-10-02 (#5)

One Python process serves the generated static interface and the API (ADR-011);
no Node.js runs in production. The shell's CSP allows scripts only from `'self'`
plus SHA-256 hashes of the two generated inline bootstrap scripts; styles allow
inline because the sandboxed `srcdoc` preview inherits the policy. Tests cover
the policy, immutable assets, symlink/traversal refusal under `/_nuxt/`, Host
validation and unchanged API headers; browser E2E fails on any CSP violation.

`scripts/docker-smoke.py` ran against a real Docker daemon (Docker Desktop
29.6.2 on WSL2, linux/amd64): 19 acceptance checks at the time of #16 (20 after
#19 added the password-reset check) covering non-root UID,
read-only root, dropped capabilities, loopback-only publishing, bind-mounted
site/state/secrets, host UID/GID on writes, external edits, authenticated
private push/fetch, token absence from inspect/logs/API/.git/state, restart and
replacement persistence, and runtime contents (no Node/uv/pip/compiler/curl/ssh
or development packages). Hadolint 2.15.1 passes.

Not verified at the time: real reverse-proxy configurations, native Linux
Docker Engine, rootless Docker or user-namespace remapping, ARM, and systemd unit
hardening. The first two are covered by the native validation below.
The Debian `git` package (2.39.5) is trusted native code patched via image rebuilds.

## Native Linux deployment validation — 2026-10-03

Host: a disposable KVM guest on the maintainer workstation (not the intended
production server) running stock Debian 12 (kernel 6.1.0-53-cloud-amd64),
Docker Engine 29.8.2 / API 1.56 from download.docker.com (rootful, overlayfs,
cgroup v2, AppArmor + seccomp), Compose v5.6.0, Caddy v2.11.7 from Caddy's apt
repository. Deployment user UID 1501 / GID 1000 (deliberately non-default and
unequal), member of the `docker` group; commit built from a git bundle on the host.

- `task docker:check`: Hadolint and proxy examples valid; image built; 23/23
  acceptance checks, including UID 1501 ownership, backup → destroy → restore
  (fail-closed start without the Git secret) and a 45 s stop timeout.
- Operator-style deployment from `docs/containers.md` behind the shipped
  Caddyfile (only the domain and `local_certs` changed, no public DNS):
  `scripts/proxy-smoke.py` 8/8; Playwright deploy spec 1/1 (no CSP violations,
  Secure/HttpOnly/SameSite=Strict `/api` cookie, forged CSRF request refused,
  empty browser storage, token absent); pushed commit present on the private
  remote; token absent from `docker inspect`, logs, site and state. An echo
  upstream confirmed Caddy forwards `Host: <domain>` unchanged and overwrites a
  client-supplied `X-Forwarded-Host`.

Review findings: Docker's default 10 s stop timeout could SIGKILL an in-flight
Git fetch/push (30 s deadline) and leave lock files, so Compose now sets
`stop_grace_period: 45s` (tested). The earlier nginx example referenced an
undefined `limit_conn` zone and commented-out certificates, so it would fail
`nginx -t`; it is replaced by the validated `deploy/nginx.conf`. Reviewed without
change: CSP hashing and asset serving (`builds/latest.json` is cache-busted by
Nuxt, so immutable caching is harmless), push porcelain parsing and fixed
`RemoteError` text, credential file handling, proxy-header non-trust, mount
modes, and `reset-admin` (one transaction). One finding was fixed afterwards:
the administrator commands shared `serve`'s full configuration validation, so a
broken remote secret blocked password recovery (see the authentication section).

Still unverified: the maintainer's actual server, ACME certificate issuance,
nginx at runtime (validated with `nginx -t` only), rootless Docker,
user-namespace remapping, ARM, and the systemd guidance.

## Container re-verification after the UX pass — 2026-10-06

PR #22 replaced the browser bundle packaged into the image. `task docker:check` on Docker Desktop 29.6.2 (WSL2, linux/amd64) against main 6c6b02b: Hadolint and both proxy examples valid, image built, 24/24 acceptance checks (image 238.8 MB; no Node/uv/pip/compiler/curl/ssh). The native-Engine and intended-production-server validations in issue #5 remain as recorded above; nothing here is a production-host validation.
