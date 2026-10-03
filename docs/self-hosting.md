# Self-hosting BurzBlog

BurzBlog is one process that serves the browser writing interface and its
authenticated API from a single origin ([ADR-011](adr/011-production-topology.md)).
It needs a content directory, a private state directory, optionally a Git remote
token, and an HTTPS reverse proxy you operate. There is no database server,
worker, queue or Node.js runtime in production.

| What | Where | Back up? |
| --- | --- | --- |
| Markdown, `.archive`, `.burzblog-recovery`, `.git` | site directory | yes — this is your content |
| Login database (SQLite) | state directory, 0700, outside the site | yes, privately |
| Git token / private CA | secrets directory, 0700, outside both | privately, or re-issue |
| Code | container image or Python package | no — reinstall a pinned version |

## Recommended: container

Follow [containers](containers.md): create three host directories, build the
image, run `init-admin`, `docker compose up -d`, and put an HTTPS reverse proxy
in front of `127.0.0.1:8080`. This is the verified deployment path.

## Without containers (systemd)

Prerequisites: Python 3.12+, uv, Git ≥ 2.31, Node 24 + pnpm 11 to build the
interface once. Use a dedicated account that owns the content.

```sh
task setup && (cd web && pnpm build)       # generates web/.output/public
export BURZBLOG_CONTENT_ROOT=/srv/burzblog/site
export BURZBLOG_STATE_DIR=/srv/burzblog/state      # 0700, outside the site
export BURZBLOG_ORIGIN=https://blog.example.org
export BURZBLOG_WEB_ROOT=$PWD/web/.output/public
uv run --frozen burzblog-server init-admin
uv run --frozen burzblog-server serve --port 8080  # binds 127.0.0.1 only
```

Run `serve` under a supervisor (systemd `Restart=on-failure`, `User=` the content
owner, `NoNewPrivileges=yes`, `ProtectSystem=strict` with `ReadWritePaths=` for
the site and state directories; these unit settings are guidance, not tested
here), and reverse-proxy to `127.0.0.1:8080` exactly as
in the [container guide](containers.md#4-put-https-in-front). Never run the Nuxt
development server (`task dev`) for real use: it is for local development only.

## Content rules

Point the content root at an existing directory owned by the service user. Root
and subdirectories must not be group/world writable, content files must have the
account's effective group, and symlinked ancestors are rejected. Create
collection folders and `.archive` yourself:

```text
site/
  posts/
  pages/
  .archive/
```

## Backups, upgrades, recovery

The step-by-step procedure is in [backup and restore](backup.md).

Back up the site (including `.archive`, `.burzblog-recovery` and `.git`) and the
state directory with normal filesystem tools while writers are stopped; restore
with the same tools. A private Git token ([setup](git.md#private-https-remotes))
can be re-issued at the Git host after a restore instead of being backed up.
Upgrades are explicit: back up, install a pinned version, start, verify, keep the
previous version for rollback. Startup never migrates Markdown. If a save or Git
transition reports recovery, follow [recovery](recovery.md) before retrying.

Performance figures in [performance](performance.md) are a local sanity baseline,
not capacity claims. Native Windows, network filesystems and crash/power-loss
fault injection are unverified.
