# ADR-011: Single-process production topology and container packaging

Accepted. Supersedes the CLI-only container of the foundation milestone (#5).

## Context

The browser interface is a Nuxt single-page application (`ssr: false`). Its
Node server only returned static files, added headers and proxied `/api/` to the
loopback Python API. Two processes meant two runtimes, a supervisor, a proxy hop
and Node's development-tool dependency chain (#11) near the deployment.

## Decision

- `pnpm build` runs `nuxt generate`: a static shell (`index.html`) plus
  content-hashed `_nuxt/` assets. No Node.js runs in production.
- `burzblog-server serve` serves that build when `BURZBLOG_WEB_ROOT` is set: `/`
  returns the shell, `/_nuxt/*` returns immutable assets (no symlinks or
  traversal), everything else stays the authenticated `/api/` surface. One origin,
  one process, no CORS. Without `BURZBLOG_WEB_ROOT` it is API-only (development
  keeps the Nuxt dev server proxying to it).
- The shell gets its own CSP: `script-src 'self'` plus SHA-256 hashes of the
  generated inline bootstrap scripts (no `unsafe-inline`/`unsafe-eval` for
  scripts), `connect-src 'self'`, `frame-ancestors 'none'`, `object-src 'none'`,
  `base-uri 'none'`. `style-src` allows inline styles because the sandboxed
  `srcdoc` preview inherits the policy and carries its own fixed stylesheet. API
  responses keep `default-src 'none'` and `no-store`.
- The built-in bind stays `127.0.0.1`. `--host 0.0.0.0` exists for containers,
  whose published port Compose binds to host loopback only.
- One image: Python runtime venv (no dev/build tools, no pip), the static build,
  Debian `git`, `ca-certificates` and `tini` (PID 1, reaps Git helpers). Node,
  pnpm and uv exist only in build stages. Base images are digest-pinned; Debian
  packages are version-pinned and fail closed when superseded.
- Persistent data is three operator-created bind mounts, never image layers or
  anonymous volumes: `/site` (Markdown and its `.git`), `/state` (0700 SQLite
  authentication) and `/secrets` (0700, read-only; Git token/CA files). Compose
  refuses to create missing host paths, so a typo cannot silently start an empty
  site. Replacing the image cannot delete any of them.
- The container runs as the operator-supplied numeric UID/GID of the content
  owner (required, no default), read-only root, tmpfs `/tmp`, all capabilities
  dropped, `no-new-privileges`, `restart: unless-stopped`.
- Public TLS, HSTS, request timeouts and connection limits belong to an external
  reverse proxy that preserves `Host` and forwards to `127.0.0.1:${BURZBLOG_PORT}`.
- Health: the image `HEALTHCHECK` requests the unauthenticated, content-free
  `/api/session`; startup itself refuses invalid configuration or an
  uninitialized administrator, so "healthy" implies configured and ready.
- Upgrades are explicit: pin an image digest, back up, replace, verify; roll back
  by restoring the previous digest. Startup never migrates Markdown; the SQLite
  schema uses `CREATE TABLE IF NOT EXISTS` only.

## Consequences

Production memory roughly halves (one ~59 MiB process instead of two) and the
node-forge advisory (#11) has no runtime presence at all. Server-side rendering
or Node middleware would require revisiting this decision. The image is
linux/amd64 only until ARM is verified. Rootless Docker and user-namespace
remapping change host ownership semantics and need separate verification.
No image is published; GHCR publication remains a maintainer action (#5, #3).
