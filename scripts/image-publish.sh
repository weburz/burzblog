#!/bin/bash
# Build, smoke-test and publish the multi-arch container image to GHCR from a
# clean checkout of a vX.Y.Z tag; a fallback for the image step of
# .github/workflows/release.yml, which publishes on tag pushes.
# Tags pushed: X.Y.Z (never overwritten), X.Y, X and latest.
# Requires Docker with buildx and `docker login ghcr.io`.
# Usage: scripts/image-publish.sh [--dry-run]
set -euo pipefail
repo=ghcr.io/weburz/burzblog
platforms=linux/amd64,linux/arm64
dry_run=false
case "${1:-}" in
  "") ;;
  --dry-run) dry_run=true ;;
  *) echo "usage: $0 [--dry-run]" >&2; exit 2 ;;
esac

cd "$(dirname "$0")/.."
test -z "$(git status --porcelain)" || { echo "refusing to publish: the working tree is not clean" >&2; exit 1; }
tag=$(git describe --tags --exact-match 2>/dev/null) || { echo "refusing to publish: HEAD is not exactly on a tag" >&2; exit 1; }
[[ $tag =~ ^v([0-9]+)\.([0-9]+)\.([0-9]+)$ ]] || { echo "refusing to publish: $tag is not a vX.Y.Z tag" >&2; exit 1; }
major=${BASH_REMATCH[1]}
minor=${BASH_REMATCH[2]}
version=${tag#v}
tags=(-t "$repo:$version" -t "$repo:$major.$minor" -t "$repo:$major" -t "$repo:latest")

if ! $dry_run && docker buildx imagetools inspect "$repo:$version" >/dev/null 2>&1; then
  echo "$repo:$version already exists; published tags are never overwritten" >&2
  exit 1
fi

docker buildx inspect burzblog-publish >/dev/null 2>&1 || docker buildx create --name burzblog-publish --driver docker-container >/dev/null
scratch=$(mktemp -d)
trap 'rm -r "$scratch"' EXIT

# Test the exact source first: the native image through the container smoke test.
docker build --build-arg "VERSION=$version" --tag burzblog:local .
scripts/docker-smoke.sh burzblog:local

if $dry_run; then
  docker buildx build --builder burzblog-publish --platform "$platforms" --build-arg "VERSION=$version" .
  echo "dry run: built $platforms; would push ${tags[*]//-t /}"
  exit 0
fi

docker buildx build --builder burzblog-publish --platform "$platforms" --build-arg "VERSION=$version" \
  --provenance=false "${tags[@]}" --metadata-file "$scratch/meta.json" --push .
sed -n 's/.*"containerimage.digest": *"\([^"]*\)".*/\1/p' "$scratch/meta.json" |
  sed "s|^|published $repo@|"
