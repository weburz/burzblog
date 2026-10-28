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

Images are published by the release workflow when a `vX.Y.Z` tag is pushed
(see [CONTRIBUTING.md](../CONTRIBUTING.md#releasing)). If GitHub Actions is
unavailable, a maintainer can publish from a clean checkout of the tag after
`docker login ghcr.io`:

```sh
task image:publish -- --dry-run   # smoke-test and build both platforms, push nothing
task image:publish                # the same, then push the four tags
```

The task refuses a dirty tree, a commit that is not exactly a `vX.Y.Z` tag and
a version that already exists, and prints the pushed digest.

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

The database and uploads live in the `burzblog-data` named volume. Coolify's
scheduled backups cover the databases it manages, not a Compose resource's
volumes, so back the blog up yourself (see [Backups](#backups)). One instance
idles at about 25 MB of memory, and the file limits it to 128 MB, which suits a
personal blog; raise `mem_limit` for heavy imports.

To update, set `BURZBLOG_VERSION` to the new `X.Y.Z` (or to `2` to follow the
latest 2.x.y on every redeploy) and redeploy. The database is migrated when the
new version starts and migrations are never undone, so take a snapshot first.

## Backups

The simplest backup is automatic: set these in the environment (in Coolify,
the resource's environment variables, marking the keys and the passphrase as
secrets) and the server sends an encrypted snapshot to S3-compatible storage
every day, sending each upload only once:

```sh
BURZBLOG_SNAPSHOT_EVERY=24h
BURZBLOG_SNAPSHOT_TO=s3://example-backups/blog-name/   # one prefix per blog
BURZBLOG_SNAPSHOT_PASSPHRASE=...                       # keep a copy off the server
BURZBLOG_S3_ENDPOINT=https://<account>.r2.cloudflarestorage.com
BURZBLOG_S3_ACCESS_KEY_ID=...
BURZBLOG_S3_SECRET_ACCESS_KEY=...
```

See [automatic snapshots](self-hosting.md#automatic-snapshots) for every
setting. Several blogs can share one bucket with different prefixes.

The image has no shell, so run BurzBlog's own commands in the container. A
snapshot holds everything: posts with every earlier version, accounts and
uploads (see [self-hosting](self-hosting.md#full-snapshots)). Stream one out
of the running container to the host, encrypted with a passphrase from the
environment:

```sh
docker compose exec -T -e BURZBLOG_SNAPSHOT_PASSPHRASE burzblog \
  /burzblog snapshot create - > blog-$(date +%F).tar.gz.enc
```

Run that from cron on another machine over `ssh`, so the copy is not on the
server's disk. On a Coolify server, find the container with
`docker ps --filter name=burzblog` and run `docker exec <container> /burzblog
snapshot create -` the same way.

To restore, stop the service and run the restore in a one-off container on the
same volume, then start it again:

```sh
docker compose stop burzblog
docker compose run --rm -T -e BURZBLOG_SNAPSHOT_PASSPHRASE burzblog snapshot restore - < blog.tar.gz.enc
docker compose start burzblog
```

From the bucket instead, pass the same `BURZBLOG_S3_*` settings and the
passphrase to the one-off container and restore `s3://example-backups/blog-name/`.

The root filesystem is read-only, so check a snapshot in the data volume:
`docker compose run --rm -T burzblog snapshot verify - --work /data < blog.tar.gz.enc`.

## Checks

`task docker:check` lints the Dockerfile with Hadolint, validates the Caddy and
nginx examples, builds the image and runs `scripts/docker-smoke.sh`. The smoke
test runs the image as your user against a throwaway data directory, then
checks the health endpoint, that anonymous API calls are refused, the panel and
its CSP, issuing a CLI token, creating and publishing a post, the export, data
surviving a restart, and the database file mode. It then runs the image the
way Coolify does, with a fresh named volume and no `--user`, and checks that it
starts, creates an account and keeps it across a restart.
