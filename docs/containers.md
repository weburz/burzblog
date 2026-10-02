# Local containers and image updates

The image currently runs the **foundation CLI**, not a web server. There are no
ports, browser UI, database or migrations. This packaging establishes the future
single-image update model while keeping ordinary content files on the host.
Docker builds and runtime smoke tests must pass before calling any image usable.
They have not been run in the sandboxed implementation environment.

## Build and use locally

Install Docker Engine with Compose v2. Create the content directory yourself;
Compose intentionally refuses to create a root-owned bind-mount source.

```sh
mkdir -p content/posts content/.archive
export BURZBLOG_UID="$(id -u)"
export BURZBLOG_GID="$(id -g)"
task docker:build
docker compose run --rm burzblog list
printf '# Hello\n' | docker compose run --rm -T burzblog create posts/hello.md --title Hello --slug hello
printf '\nExternal edit.\n' >> content/posts/hello.md
docker compose run --rm burzblog read posts/hello.md
task docker:smoke
```

The smoke task uses a separate temporary host directory and leaves its path for
inspection. It checks create → file → external edit → read and numeric ownership.
Run as an ordinary unprivileged host user. Do not use `sudo task`: it changes the
content owner. Files must match the effective UID/GID passed into the container.
No recursive chown is performed. Rootless Docker/user-namespace remapping and
Docker Desktop mounts require separate ownership verification.

The runtime is non-root, read-only apart from `/content`, has no network, drops
capabilities and forbids privilege escalation. Base Python and uv images are
version-and-digest pinned. The final image contains runtime dependencies only;
uv, build tooling and application tests stay in the build stage. Build context
is allowlisted so `.env`, content, `.git` and legacy files are not sent to Docker.

## Use a published image without cloning

No image has been published by this change. After a verified image exists, save
`compose.dist.yaml` beside your content directory and `.env`. Set:

```text
BURZBLOG_IMAGE=ghcr.io/weburz/burzblog@sha256:<published-image-digest>
BURZBLOG_UID=<your numeric UID>
BURZBLOG_GID=<your numeric GID>
BURZBLOG_CONTENT_DIR=./content
```

```sh
docker compose -f compose.dist.yaml pull
docker compose -f compose.dist.yaml run --rm burzblog list
```

To upgrade: stop writers, back up content (including `.archive` and any `.git`),
record the old image digest, change `BURZBLOG_IMAGE`, pull, then run a read/list
check. Subsequent CLI runs use the new image. Roll back by restoring the previous
digest; content is independent of container replacement. Do not use an unattended
updater or assume a `latest` tag. A future persistent web service can use
`pull` followed by `up -d` once that service actually exists.

## Publishing workflow (dormant)

GitHub Actions is intentionally disabled. Do not enable it without maintainer
direction. `.github/workflows/image.yml` is a manual main-branch workflow: local
quality gates → Hadolint → build linux/amd64 image → smoke test → optional GHCR
push of that exact image. Its default is build/test only. Push uses the short-lived
GitHub token and tags `sha-<full-commit>`; consumers should pin the resulting digest.
Do not overwrite published commit tags. ARM images are deferred until tested.

Before public distribution, resolve the project license and dependency notices
(issue #3). A maintainer must also set GHCR package visibility if anonymous pulls
are desired. Private packages require a pull credential in Docker's credential
store, never the content directory. No publishing, package-visibility change or
Actions re-enablement was performed here. When Actions is re-enabled, its current
allowlist must include the pinned Task and Docker actions in these workflows.
