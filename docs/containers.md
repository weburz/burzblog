# Containers

The plain binary under systemd is the simplest way to run BurzBlog (see
[self-hosting](self-hosting.md)). If you prefer containers, the image holds the
same single binary on a distroless base, under 20 MB.

## Build and run with Compose

```sh
cp .env.example .env          # set BURZBLOG_ORIGIN, BURZBLOG_UID/GID
mkdir -m 700 data             # owned by BURZBLOG_UID
task docker:build
docker compose up -d
docker compose logs burzblog       # open the "/setup#token=..." link it prints
```

On a fresh `data/` the server logs a one-time setup link (valid for 60
minutes; `docker compose restart burzblog` prints a new one). Open it to create
the first account in the browser; the address uses `BURZBLOG_ORIGIN`, so set
that to the URL you will open. To create the account from a terminal instead,
run `docker compose run --rm burzblog user add ada` before or after starting.

The container:

- runs as the numeric owner of `data/` with a read-only root filesystem, no
  capabilities and `no-new-privileges`;
- keeps everything in `/data` (the SQLite database), a bind mount here and a
  named volume elsewhere (the image ships `/data` owned by the runtime user,
  so a new named volume is writable without a `--user` override), so
  replacing the image never touches your posts;
- publishes port 8080 on host loopback only, for your HTTPS reverse proxy;
- reports health with `burzblog healthcheck` against `/healthz`.

`compose.dist.yaml` is the same service for a published image; pin
`BURZBLOG_IMAGE` to a digest.

## Published images

Each release is published to `ghcr.io/weburz/burzblog` for `linux/amd64` and
`linux/arm64` with the tags `X.Y.Z` (never overwritten), `X.Y`, `X` and
`latest`. Pin `X.Y.Z` or a digest in production; a floating tag follows new
releases whenever the container is pulled again. Pulling without logging in
needs the GHCR package to be public (package settings, Change visibility).

A maintainer publishes from a clean checkout of the `vX.Y.Z` tag after
`docker login ghcr.io`:

```sh
task image:publish -- --dry-run   # smoke-test and build both platforms, push nothing
task image:publish                # the same, then push the four tags
```

The task refuses a dirty tree, a commit that is not exactly a `vX.Y.Z` tag and
a version that already exists, and prints the pushed digest. The dormant
`.github/workflows/image.yml` does the same on a tag.

## Coolify

[Coolify](https://coolify.io) is a self-hosted PaaS. Coolify's one-click
service catalogue does not list BurzBlog yet, so deploy it as a custom
Compose resource:

1. In a project, add a resource: Docker Compose Empty.
2. Paste the contents of [`deploy/coolify.yaml`](../deploy/coolify.yaml).
3. Open the `burzblog` service settings and set its domain, for example
   `https://blog.example.org`. Coolify routes it to port 8080 and issues the
   certificate. `SERVICE_URL_BURZBLOG_8080` makes Coolify generate that URL
   and `BURZBLOG_ORIGIN` is set from it, so there is nothing to copy by hand.
4. Deploy, then open the service logs. On an empty database the server prints
   `open https://.../setup#token=...`; open that link within 60 minutes to
   create the first account (restart the service for a new link).

Optional settings go in the environment variables of the resource:
`BURZBLOG_SITE_URL` and `BURZBLOG_DEPLOY_HOOK` (uncomment them in the file; the
hook is a secret, mark it as such), and `BURZBLOG_VERSION` to choose the image.

The database lives in the `burzblog-data` named volume; include it in your
Coolify backups. One instance idles at about 25 MB of memory, and the file
limits it to 128 MB, which suits a personal blog; raise `mem_limit` for heavy
imports.

To update, set `BURZBLOG_VERSION` to the new `X.Y.Z` (or to `2` to follow the
latest 2.x.y on every redeploy) and redeploy. The database is migrated when the
new version starts and migrations are never undone, so take a backup first.

## Checks

`task docker:check` lints the Dockerfile with Hadolint, validates the Caddy and
nginx examples, builds the image and runs `scripts/docker-smoke.sh`. The smoke
test runs the image as your user against a throwaway data directory, then
checks the health endpoint, that anonymous API calls are refused, the panel and
its CSP, issuing a CLI token, creating and publishing a post, the export, data
surviving a restart, and the database file mode. It then runs the image the
way Coolify does, with a fresh named volume and no `--user`, and checks that it
starts, creates an account and keeps it across a restart.
