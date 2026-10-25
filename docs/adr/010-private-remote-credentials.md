# ADR-010: Provider-independent private HTTPS remote credentials

> **Superseded by [ADR-013](013-database-storage.md)** (2026-10-24). Kept as the record of the file-first design.

Accepted. Resolves issue #13 within the ADR-006/007 Git boundary.

## Scope: HTTPS tokens now, SSH deferred

Support one operator-configured HTTPS remote authenticated with a username and
a token/password read from a private file. Every common host and self-hosted
forge (GitHub, GitLab, Bitbucket, Gitea/Forgejo, plain `git http-backend`)
accepts HTTP Basic authentication with a scoped access token, so no provider
API, OAuth flow or account linking enters the core. Tokens can be repository-
scoped and revoked by the operator at the host.

SSH is deferred. Safe SSH would need BurzBlog-owned host-key pinning
(`known_hosts`), private-key and passphrase handling, agent isolation and an
audited `ssh` invocation that ignores `~/.ssh/config` (`ProxyCommand`,
`LocalCommand`, `Match exec` run arbitrary commands). That is a separate,
larger trust surface. Operators who need SSH continue to use ordinary Git.

## Decision

- Configuration is operator-only: `BURZBLOG_REMOTE_URL`, `BURZBLOG_REMOTE_NAME`,
  `BURZBLOG_REMOTE_USERNAME`, `BURZBLOG_REMOTE_TOKEN_FILE` and optional
  `BURZBLOG_REMOTE_CA_FILE` (CLI: `--username/--token-file/--ca-file`). The
  browser API accepts no URL, username, path or secret; unknown fields are 422.
- The secret lives in a file, never in an environment variable. The file must
  be an absolute, symlink-free, regular file owned by the service user with no
  group/other permission bits (0600/0400), at most 4 KiB, exactly one UTF-8 line
  without control characters. It must lie outside the content root, its Git
  repository and the authentication state directory. Inline
  `BURZBLOG_REMOTE_TOKEN`/`_PASSWORD` variables are refused at startup.
- The file is re-read for each fetch/push (rotation without restart) and
  validated at startup. The secret is never stored on Python objects.
- It reaches Git as `http.<exact-url>.extraHeader=Authorization: Basic …`
  through `GIT_CONFIG_COUNT/KEY/VALUE` in the child environment (Git ≥ 2.31,
  checked at startup). Not in argv (world-readable via `/proc`), not in URLs,
  not in `.git/config`, not through a credential helper or askpass program.
- The header is URL-scoped; redirects remain disabled, so it cannot be
  forwarded to another host. TLS verification remains mandatory; a private
  certificate authority is an explicit PEM bundle that only its owner (or root)
  can modify. HTTP, `ssh://`, scp-style and local paths cannot carry credentials.
- The Git environment is rebuilt from scratch: `HOME=/dev/null`, no system/global
  config, no inherited `GIT_*`/askpass/proxy variables, `credential.helper` reset,
  terminal prompts off. Ambient `~/.gitconfig`, `~/.git-credentials` and
  `~/.netrc` are therefore never discovered.
- Repository configuration keeps the ADR-006 allowlist: credential helpers,
  `http.*`, `url.*.insteadOf`, upload/receive-pack commands, `core.askPass`,
  `core.sshCommand` and includes are refused before any network contact.
- Git stderr stays discarded. Remote failures become a fixed `RemoteError`
  (API: 502 `remote`), distinct from a push the remote refuses — non-fast-forward
  or a protected branch — (409 conflict, detected from `push --porcelain`
  reference flags). Pushes remain non-forced. Credential misconfiguration found
  at runtime is also a `remote` failure and is detected before Git starts.

## Threat model

Protected against: browser users or a stolen session choosing endpoints or
exfiltrating the secret through API responses; secrets reaching content, Git
history, `.git/config`, logs, argv or error text; hostile repository
configuration executing helpers or redirecting transport; credential forwarding
via redirects or plaintext HTTP; silent use of ambient user credentials.

Trusted: the operator, the OS account running BurzBlog (it can read the secret
file and `/proc/<pid>/environ` of its own Git children), root, the Git binary
and libcurl, and the configured remote host. A compromised remote can refuse or
serve history but cannot make BurzBlog overwrite divergent local content.
A stolen session can still push already-committed content to the configured
remote; scope tokens to one repository and revoke them at the host.

Not provided: SSH, multiple remotes, per-user credentials, OAuth/device flows,
outbound proxies (proxy variables are not inherited), client certificates, or
encrypted-at-rest secret storage beyond filesystem permissions.
