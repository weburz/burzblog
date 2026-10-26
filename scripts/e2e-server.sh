#!/bin/sh
# A disposable BurzBlog for browser tests: fresh data directory, one account,
# the production single-process topology on 127.0.0.1:${E2E_PORT:-3000}.
set -eu
root=$(cd "$(dirname "$0")/.." && pwd)
port=${E2E_PORT:-3000}
work=${BURZBLOG_E2E_ROOT:?set BURZBLOG_E2E_ROOT to an empty temporary directory}
"$root/scripts/embed-panel.sh"
go -C "$root" build -o "$work/burzblog" ./cmd/burzblog
printf '%s\n' "browser fixture password" | "$work/burzblog" user add e2e --role admin --data "$work/data"
exec "$work/burzblog" serve --addr "127.0.0.1:$port" --origin "http://127.0.0.1:$port" --data "$work/data"
