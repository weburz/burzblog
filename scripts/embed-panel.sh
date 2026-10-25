#!/bin/sh
# Copy the generated browser panel (web/.output/public) into the Go embed
# directory, replacing any earlier build. Run after `pnpm --dir web build`.
set -eu
root=$(cd "$(dirname "$0")/.." && pwd)
src="$root/web/.output/public"
dst="$root/internal/panel/dist"
if [ ! -f "$src/index.html" ]; then
  echo "embed-panel: $src/index.html is missing; run pnpm --dir web build first" >&2
  exit 1
fi
find "$dst" -mindepth 1 ! -name .gitkeep -delete
cp -R "$src/." "$dst/"
echo "embed-panel: copied the panel into internal/panel/dist"
