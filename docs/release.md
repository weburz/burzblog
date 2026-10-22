# Release process

Maintainer procedure for cutting a BurzBlog release.

## Preconditions

- Review the product scope and public-distribution gates in [launch.md](launch.md).
- Verify the publication example and draft exclusion from [publishing.md](publishing.md).

- `LICENSE` (Apache-2.0) is present and `pyproject.toml` carries the release version.
- `task check` and `task docker:check` pass on the release commit.
- The production-host validation from [production-runbook.md](production-runbook.md)
  is recorded in issue #5.
- `CHANGELOG.md` `[Unreleased]` is moved under the version heading with a date.
- Action pins in `.github/workflows/*.yml` must be full commit SHAs (an annotated tag's object SHA does not resolve); verify with `gh api repos/<owner>/<repo>/commits/<sha>` after any bump.

## Versioning

- Semantic Versioning. `version` in `pyproject.toml` is the single source of truth.
- 1.0.0 is the first release.
- The Git tag is `v<version>` and is annotated.
- Image tags are `v<version>` and `sha-<commit>`. There is no `latest` tag.
- Deployments pin the image digest, not a tag.
- The wheel and sdist from `task build` carry the same version.

## Cut the release

1. Bump `version` in `pyproject.toml` and update `CHANGELOG.md` in one PR; merge it.
2. Tag the merge commit and push the tag:

   ```sh
   git tag -a v1.0.0 -m "BurzBlog 1.0.0"
   git push origin v1.0.0
   ```

3. Enable GitHub Actions (maintainer only; it is intentionally disabled until
   this point, see `AGENTS.md`).
4. Run the "Container image" workflow from the tag with `publish` = true. It
   refuses a tag that differs from `pyproject.toml`, refuses to overwrite an
   existing version tag, and writes the digest of each pushed tag to the job summary.
5. Copy the digest into the CHANGELOG entry, write the release notes from that
   entry to a temporary file, and create the GitHub release from the tag:

   ```sh
   gh release create v1.0.0 --verify-tag --notes-file /tmp/burzblog-1.0.0-notes.md
   ```

   Attaching `dist/*.whl` and `dist/*.tar.gz` from `task build` is optional.
6. Set GHCR package visibility deliberately.

## Upgrade and rollback expectations

See [containers.md](containers.md#upgrade-and-rollback). Within 1.x, startup never
migrates Markdown and the state schema is created only if absent, so rolling back
to an earlier 1.x image is safe unless a release note says otherwise.

## What a release does not do

No telemetry, no automatic update, and no `latest` tag.
