#!/usr/bin/env bash
set -euo pipefail

# The caller builds this exact image first. Keep this separate from task check
# because Docker is unavailable in sandboxed development environments.
image_ref="${BURZBLOG_TEST_IMAGE:-burzblog:local}"
fixture_dir="$(mktemp -d -t burzblog-container-XXXXXX)"
# Retain the isolated fixture for inspection, including after a failure.
trap 'printf "Container smoke fixture: %s\n" "$fixture_dir"' EXIT
container_args=(--rm --network none --read-only --cap-drop ALL
  --security-opt no-new-privileges:true --user "$(id -u):$(id -g)"
  --mount "type=bind,source=$fixture_dir,target=/content")
printf '# Hello\n' | docker run -i "${container_args[@]}" "$image_ref" \
  create hello.md --title Hello --slug hello
test -s "$fixture_dir/hello.md"
printf '\nExternal edit.\n' >> "$fixture_dir/hello.md"
docker run "${container_args[@]}" "$image_ref" read hello.md |
  python3 -c 'import json, sys; document = json.load(sys.stdin); assert "External edit." in document["markdown"]'
test "$(stat -c %u "$fixture_dir/hello.md")" = "$(id -u)"
printf 'Container create, external edit, fresh read and UID preservation passed.\n'
