#!/bin/sh
# Container acceptance test: run the image against a disposable data
# directory as the invoking user, then sign in, write, publish and export
# through the API. Requires Docker and curl.
set -eu
image=${1:-burzblog:local}
work=$(mktemp -d)
name="burzblog-smoke-$$"
port=18080
cleanup() {
  docker rm -f "$name" >/dev/null 2>&1 || true
  rm -r "$work"
}
trap cleanup EXIT
mkdir -m 700 "$work/data"
uid=$(id -u)
gid=$(id -g)
origin="http://127.0.0.1:$port"

printf 'smoke test password\n' | docker run --rm -i --user "$uid:$gid" --read-only \
  -v "$work/data:/data" "$image" user add smoke >/dev/null
docker run -d --name "$name" --user "$uid:$gid" --read-only --cap-drop ALL \
  --security-opt no-new-privileges:true -e BURZBLOG_ORIGIN="$origin" \
  -p "127.0.0.1:$port:8080" -v "$work/data:/data" "$image" >/dev/null

for _ in $(seq 1 50); do
  curl -fsS "$origin/healthz" >/dev/null 2>&1 && break
  sleep 0.2
done
check() { printf '  ok  %s\n' "$1"; }

curl -fsS "$origin/healthz" | grep -q '"ok"' && check "health endpoint answers"
test "$(curl -s -o /dev/null -w '%{http_code}' "$origin/api/posts")" = 401 && check "API refuses anonymous requests"
curl -fsS -D "$work/h" "$origin/" -o "$work/index.html"
grep -qi '^content-security-policy:' "$work/h" && check "panel is served with a CSP"
grep -q '_nuxt/' "$work/index.html" && check "panel assets are embedded"

token=$(curl -fsS -H 'Content-Type: application/json' \
  -d '{"username":"smoke","password":"smoke test password","name":"smoke"}' \
  "$origin/api/auth/token" | sed -n 's/.*"secret":"\([^"]*\)".*/\1/p')
test -n "$token" && check "CLI token issued"
auth="Authorization: Bearer $token"
printf -- '---\ntitle: Smoke\nstatus: published\n---\n\nHello from the container.\n' |
  curl -fsS -X PUT -H "$auth" -H 'If-None-Match: *' --data-binary @- "$origin/api/posts/smoke/markdown" >/dev/null
check "post created and published"
curl -fsS -H "$auth" "$origin/api/export.tar.gz" | tar -xzO smoke.md | grep -q 'Hello from the container.' && check "export contains the post"

docker restart "$name" >/dev/null
for _ in $(seq 1 50); do
  curl -fsS "$origin/healthz" >/dev/null 2>&1 && break
  sleep 0.2
done
curl -fsS -H "$auth" "$origin/api/posts/smoke" | grep -q '"status":"published"' && check "data survives a restart"
test "$(stat -c %a "$work/data/burzblog.db")" = 600 && check "database file is private (0600)"
test "$(docker inspect -f '{{.State.Health.Status}}' "$name" 2>/dev/null || echo none)" != unhealthy && check "container health check is not failing"
echo "docker smoke: all checks passed"
