#!/bin/sh
# Print the GitHub release notes for a version: its CHANGELOG section, then how
# to install it. Used by .github/workflows/release.yml.
# Usage: scripts/release-notes.sh X.Y.Z
set -eu
version=${1:?usage: $0 X.Y.Z}
cd "$(dirname "$0")/.."
section=$(awk -v v="$version" '
  index($0, "## [" v "]") == 1 { on = 1; next }
  on && /^## \[/ { exit }
  on { print }
' CHANGELOG.md)
if [ -z "$(printf '%s' "$section" | tr -d '[:space:]')" ]; then
  echo "release-notes: CHANGELOG.md has no \"## [$version]\" section" >&2
  exit 1
fi
printf '%s\n' "$section" | sed -e '/./,$!d' -e 's/^### /## /'
cat <<NOTES

## Install

\`\`\`sh
curl -fsSL https://burzblog.weburz.com/install.sh | sh
\`\`\`

Or download the file for your computer below and put it on your PATH:
\`linux-amd64\` (Linux, Intel/AMD), \`linux-arm64\` (Raspberry Pi 4/5 and other
64-bit ARM), \`linux-arm\` (Raspberry Pi, 32-bit OS), \`darwin-arm64\` (Apple
silicon), \`darwin-amd64\` (Intel Mac) or \`windows-amd64.exe\`. Check it with
\`sha256sum -c SHA256SUMS-v$version --ignore-missing\`. A running server updates
itself with \`burzblog upgrade\`.

Container image: \`ghcr.io/weburz/burzblog:$version\` (also \`${version%.*}\`,
\`${version%%.*}\` and \`latest\`) for \`linux/amd64\` and \`linux/arm64\`. Guides:
https://burzblog.weburz.com/docs/.
NOTES
