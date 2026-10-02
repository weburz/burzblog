# Running BurzBlog in a container

The image runs the BurzBlog browser application: one non-root process serves the
writing interface and its authenticated API on port 8080 inside the container.
Your Markdown, its Git repository, the login database and Git secrets all stay in
host directories you own; the image holds only code. See
[ADR-011](adr/011-production-topology.md) for the design.

```text
browser ──HTTPS──▶ your reverse proxy ──▶ 127.0.0.1:8080 ──▶ container (UID = you)
                                                              ├─ /site     rw  Markdown + .git
                                                              ├─ /state    rw  login database (0700)
                                                              └─ /secrets  ro  Git token / CA (0700)
```

No image has been published yet; build it locally (below). Linux amd64 with
Docker Engine and Compose v2 is the verified platform.

## 1. Prepare host directories

Run as the ordinary account that owns your content — never with `sudo`. The
container runs with this account's numeric UID/GID and refuses files owned by
anyone else.

```sh
mkdir -p ~/burzblog && cd ~/burzblog
install -d -m 0700 state secrets
# New site: plain folders are enough.
mkdir -p site/posts site/.archive
# Existing Git repository instead: clone or move it to ./site, e.g.
#   git clone https://git.example.org/owner/site.git site
# and set BURZBLOG_CONTENT_SUBDIR below if Markdown lives in a subfolder.
chmod go-w site
```

`site`, its subfolders and `.git` must not be group/world-writable. `state` and
`secrets` must be mode 0700. Keep all three on a local filesystem (not NFS).

## 2. Build the image and copy the deployment files

```sh
# In a reviewed BurzBlog checkout:
task docker:build                       # docker build --tag burzblog:local .
cp compose.dist.yaml ~/burzblog/compose.yaml
cp .env.example ~/burzblog/.env
```

The deployment directory needs only `compose.yaml`, `.env` and the three data
directories — no source checkout. (`compose.yaml` in the repository is the same
service with a `build:` section, for development.)

## 3. Configure, initialize, start

Edit `~/burzblog/.env`:

- `BURZBLOG_IMAGE=burzblog:local` (later: a published `…@sha256:` digest).
- `BURZBLOG_UID`/`BURZBLOG_GID`: output of `id -u` / `id -g`. Required.
- `BURZBLOG_ORIGIN`: the exact address people type, e.g. `https://blog.example.org`.
  For a local trial use `http://127.0.0.1:8080` and open exactly that URL —
  `localhost` and `127.0.0.1` are different origins and login will be refused.
- `BURZBLOG_CONTENT_SUBDIR`: `content` if the Markdown root is `site/content`.
- Optional Git remote: see [private remotes](#private-git-remote).

`.env` contains no secrets; never add a token or password to it.

```sh
cd ~/burzblog
docker compose run --rm burzblog init-admin   # prompts for the password twice
docker compose up --detach
docker compose ps                             # STATUS shows (healthy)
```

The service refuses to start, with a specific message in `docker compose logs`,
if the administrator is not initialized, a directory has the wrong owner or mode,
the origin is malformed, or a secret file is exposed. Fix the cause; the restart
policy retries automatically.

## 4. Put HTTPS in front

The port is published on host loopback only. Terminate TLS in a reverse proxy on
the same host, forward to `127.0.0.1:8080`, **preserve the Host header**, and set
timeouts, a body limit (≥ 4 MB) and connection limits. Proxy headers are not
trusted by BurzBlog. Examples (adapt; not tested in this repository):

```caddyfile
blog.example.org {
	request_body {
		max_size 4MB
	}
	header Strict-Transport-Security "max-age=31536000"
	reverse_proxy 127.0.0.1:8080
}
```

```nginx
server {
    listen 443 ssl;
    server_name blog.example.org;
    # ssl_certificate ... ;
    client_max_body_size 4m;
    client_body_timeout 30s;
    limit_conn perip 20;   # with: limit_conn_zone $binary_remote_addr zone=perip:10m;
    add_header Strict-Transport-Security "max-age=31536000" always;
    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_read_timeout 60s;
    }
}
```

Do not expose port 8080 publicly and do not change the Compose port binding to
`0.0.0.0`. BurzBlog has one administrator and is not a hardened multi-tenant
Internet service; see [SECURITY.md](../SECURITY.md).

## Private Git remote

Store the token as described in [private HTTPS remotes](git.md#private-https-remotes),
but in `./secrets` (mode 0600, owned by `BURZBLOG_UID`). Then in `.env`:

```sh
BURZBLOG_REMOTE_URL=https://git.example.org/owner/site.git
BURZBLOG_REMOTE_USERNAME=site-bot
BURZBLOG_REMOTE_TOKEN_FILE=/secrets/git-token   # path inside the container
# BURZBLOG_REMOTE_CA_FILE=/secrets/internal-ca.pem
```

`docker compose up --detach` applies the change. Rotating the token is just
rewriting the file; no restart. The token never enters the image, the container
environment, logs or API responses. Commit identity comes from the repository's
own `user.name`/`user.email` (`git -C site config user.name …`).

## Command-line use

The same image runs the CLI against the mounted site:

```sh
docker compose run --rm --entrypoint burzblog burzblog --root /site list
```

## Backups

Stop writers (`docker compose stop`), then back up `site` (including `.archive`,
`.burzblog-recovery` and `.git`) and `state` with ordinary tools, preserving
modes. Back up `secrets` separately with restrictive permissions, or re-issue the
token after a restore. Restarting: `docker compose start`.

## Upgrade and rollback

1. Note the current image (`docker compose images` / its digest).
2. Back up as above.
3. Build or pull the new image under a new, immutable reference and point
   `BURZBLOG_IMAGE` at it: a published digest, or for local builds a commit tag
   (`docker build --tag burzblog:$(git rev-parse --short HEAD) .`) so the
   previous image stays available. Never use an unattended updater or a moving
   `latest` tag.
4. `docker compose up --detach`, wait for `(healthy)`, sign in, open a post.
5. Roll back by restoring the previous image reference and `up --detach` again.

Replacing the container never touches `site`, `state` or `secrets`. Startup does
not migrate Markdown, and the login database schema is created only if absent,
so rolling back to an earlier image of this layout is safe. Restore `state` from
backup if a future release notes a schema change.

## Troubleshooting

- **Login says to reload/verify:** `BURZBLOG_ORIGIN` must equal the browser's
  address bar origin exactly, and the proxy must preserve `Host`.
- **"must be privately owned" / permission errors:** host owner must equal
  `BURZBLOG_UID:BURZBLOG_GID`; `state`/`secrets` mode 0700, token 0600, no
  group/world-writable site directories. Never `chmod 777` or `chown -R` to root.
- **"Remote unavailable" in the Repository view:** URL, network, token expiry or
  scopes. Test with ordinary Git from the host. Proxy variables are not inherited.
- **Docker Desktop / rootless Docker / user-namespace remapping:** bind-mount
  ownership may be translated; verify that files created in the browser appear
  on the host owned by your UID before relying on it.

## Verification

`task docker:check` lints the Dockerfile (Hadolint), builds the image and runs
`scripts/docker-smoke.py`, which uses `compose.dist.yaml` with disposable host
directories and a disposable authenticated HTTPS Git server on the Compose
network. It verifies HEALTHCHECK readiness, CSP-protected shell and assets,
login/list/read/save/create on the bind mount, host UID/GID on written files,
external host edits and stale-save refusal, authenticated push/fetch, revoked
token handling and rotation, non-root UID, read-only root/secrets with tmpfs
`/tmp`, dropped capabilities, loopback-only port, token absence from
inspect/logs/API/.git/state, persistence across restart and container
replacement, CLI use, graceful stop, image size and the absence of Node, uv, pip,
compilers, curl, ssh and development Python packages.

Static checks in `task check` keep `compose.yaml` and `compose.dist.yaml`
identical apart from build/image and enforce the hardening and bind-mount rules.

## Publishing workflow (dormant)

GitHub Actions is intentionally disabled. Do not enable it without maintainer
direction. `.github/workflows/image.yml` is a manual main-branch workflow: local
quality gates → Hadolint → build linux/amd64 image → `task docker:smoke` →
optional GHCR push of that exact image. Its default is build/test only. Push uses
the short-lived GitHub token and tags `sha-<full-commit>`; consumers should pin
the resulting digest. Do not overwrite published commit tags. ARM images are
deferred until tested.

Before public distribution, resolve the project license and dependency notices
(issue #3). A maintainer must also set GHCR package visibility if anonymous pulls
are desired. Private packages require a pull credential in Docker's credential
store, never the site directory. No publishing, package-visibility change or
Actions re-enablement has been performed.
