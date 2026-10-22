# Launch readiness

BurzBlog is a single-administrator, file-first editorial CMS for people who own
their Markdown and operate a separate reader website. It is not a managed blog
host, multi-user editorial system, or all-in-one website builder.

## What users can install

The source is Apache-2.0 licensed. The configured first version is 1.0.0; this
does not mean a release or public image has been published. Until the release
procedure is completed, build locally following [containers.md](containers.md).
Do not advertise an anonymous image pull command until its digest and public
visibility have been verified.

Application updates replace the image; content, SQLite login state, and secrets
remain in separate persistent host directories. Follow the documented backup,
upgrade, and rollback procedure. Articles are not stored in the login database.
The [publication contract](publishing.md) connects content to a separate reader
without exposing the editorial directory or adding a public content API.

## Supported and unverified deployment paths

The documented deployment is a non-root container on a local POSIX filesystem,
behind a same-origin HTTPS reverse proxy. Existing validation evidence and its
platform limits are in [containers.md](containers.md) and issue #5.

Container management platforms can be evaluated against the same deployment
contract, but no Coolify-specific certification is claimed. Before documenting
one as supported, verify all of these on a disposable instance:

- Persistent mounts for content and private state, plus read-only secrets.
- Numeric UID/GID ownership and mode checks, preserved across upgrades.
- Administrator initialization and password reset using an interactive command.
- The exact HTTPS origin and preserved Host header through the platform proxy.
- No directly public backend port; health check and graceful shutdown.
- Backup/restore, container replacement, and rollback with content retained.
- The proxy smoke and deployed browser tests from the production runbook.

## Remaining release gates

1. Run `task check` on the exact candidate commit and record actual results.
2. Run `task docker:check` where a Docker daemon is available. Container and proxy
   runtime tests are not replaced by Python or browser checks.
3. Complete [production-runbook.md](production-runbook.md) on the intended host
   and record the result in issue #5.
4. Review [dependency limitations](dependencies.md), known transport limits,
   and the media/deployment limitations in [publishing.md](publishing.md).
5. Follow [release.md](release.md): reviewed release commit, annotated tag,
   maintainer-enabled image workflow, verified image digest, release notes.
6. Before making the repository public, review tracked files **and history** for
   private data and credentials. Repository visibility and GHCR package visibility
   are separate maintainer actions. Verify anonymous access to both deliberately.

GitHub Actions remains disabled until the maintainer explicitly enables it.
Preparing release code or documentation does not change visibility, publish an
image, or satisfy production-host acceptance.
