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
- keeps everything in the bind-mounted `/data` (the SQLite database), so
  replacing the image never touches your posts;
- publishes port 8080 on host loopback only, for your HTTPS reverse proxy;
- reports health with `burzblog healthcheck` against `/healthz`.

`compose.dist.yaml` is the same service for a published image; pin
`BURZBLOG_IMAGE` to a digest.

## Checks

`task docker:check` lints the Dockerfile with Hadolint, validates the Caddy and
nginx examples, builds the image and runs `scripts/docker-smoke.sh`. The smoke
test runs the image as your user against a throwaway data directory, then
checks the health endpoint, that anonymous API calls are refused, the panel and
its CSP, issuing a CLI token, creating and publishing a post, the export, data
surviving a restart, and the database file mode.
