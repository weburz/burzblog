#!/bin/sh
# Runs site/install.sh against a local fake release server (python3 http.server)
# and a fake uname. Covers success, checksum mismatch, unknown arch, explicit
# version and the latest-release redirect.
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
installer=$root/site/install.sh
work=$(mktemp -d "${TMPDIR:-/tmp}/burzblog-install-test.XXXXXX")
server_pid=
cleanup() {
  [ -z "$server_pid" ] || kill "$server_pid" 2>/dev/null || true
  rm -rf "$work"
}
trap cleanup EXIT INT TERM HUP

command -v python3 >/dev/null 2>&1 || { echo "install-test: python3 is required" >&2; exit 1; }

if command -v sha256sum >/dev/null 2>&1; then
  sum() { sha256sum "$1" | cut -d ' ' -f 1; }
else
  sum() { shasum -a 256 "$1" | cut -d ' ' -f 1; }
fi

# Fake releases: v1.0.0 (good) and v2.0.0 (latest, good) for linux-amd64, plus
# v3.0.0 whose SHA256SUMS does not match its binary.
site=$work/site
for v in v1.0.0 v2.0.0 v3.0.0; do
  d=$site/releases/download/$v
  mkdir -p "$d"
  printf '#!/bin/sh\necho "burzblog %s"\n' "$v" >"$d/burzblog-$v-linux-amd64"
  if [ "$v" = v3.0.0 ]; then
    printf '%064d  burzblog-%s-linux-amd64\n' 0 "$v" >"$d/SHA256SUMS-$v"
  else
    printf '%s  burzblog-%s-linux-amd64\n' "$(sum "$d/burzblog-$v-linux-amd64")" "$v" >"$d/SHA256SUMS-$v"
  fi
done

cat >"$work/server.py" <<'PY'
import http.server
import sys


class Handler(http.server.SimpleHTTPRequestHandler):
    def do_HEAD(self):
        if self.path == "/releases/latest":
            self.send_response(302)
            self.send_header("Location", "http://%s/releases/tag/v2.0.0" % self.headers["Host"])
            self.end_headers()
            return
        super().do_HEAD()

    def log_message(self, *args):
        pass


server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
print(server.server_address[1], flush=True)
server.serve_forever()
PY
(cd "$site" && exec python3 "$work/server.py") >"$work/port" &
server_pid=$!
i=0
while [ ! -s "$work/port" ]; do
  i=$((i + 1))
  [ "$i" -le 50 ] || { echo "install-test: server did not start" >&2; exit 1; }
  sleep 0.1
done
base=http://127.0.0.1:$(cat "$work/port")/releases

# Fake uname so the result does not depend on the host.
fakebin=$work/fakebin
mkdir -p "$fakebin"
cat >"$fakebin/uname" <<'UNAME'
#!/bin/sh
case "$1" in
  -s) echo "${FAKE_OS:-Linux}" ;;
  -m) echo "${FAKE_ARCH:-x86_64}" ;;
esac
UNAME
chmod +x "$fakebin/uname"

failures=0
pass() { echo "ok   $1"; }
bad() { echo "FAIL $1"; failures=$((failures + 1)); }

# run NAME [VAR=VALUE ...] runs the installer into a fresh dir; output in $work/out.
run() {
  rm -rf "${work:?}/bin"
  env PATH="$fakebin:$PATH" BURZBLOG_BASE_URL="$base" BURZBLOG_INSTALL_DIR="$work/bin" "$@" \
    sh "$installer" >"$work/out" 2>&1
}

if run BURZBLOG_VERSION=v1.0.0 && [ -x "$work/bin/burzblog" ] && grep -q "burzblog v1.0.0" "$work/out"; then
  pass "explicit version installs"
else
  bad "explicit version installs"; cat "$work/out"
fi

if run && grep -q "burzblog v2.0.0" "$work/out"; then
  pass "latest resolves via redirect"
else
  bad "latest resolves via redirect"; cat "$work/out"
fi

if run BURZBLOG_VERSION=v3.0.0; then
  bad "checksum mismatch is refused"
elif [ ! -e "$work/bin/burzblog" ] && grep -q "checksum mismatch" "$work/out"; then
  pass "checksum mismatch is refused"
else
  bad "checksum mismatch is refused"; cat "$work/out"
fi

if run BURZBLOG_VERSION=v1.0.0 FAKE_ARCH=riscv64; then
  bad "unknown arch errors"
elif grep -q "unsupported architecture: riscv64" "$work/out" && [ ! -e "$work/bin/burzblog" ]; then
  pass "unknown arch errors"
else
  bad "unknown arch errors"; cat "$work/out"
fi

if run BURZBLOG_VERSION=v1.0.0 FAKE_OS=MINGW64_NT; then
  bad "windows prints download URL and fails"
elif grep -q "windows-amd64.exe" "$work/out"; then
  pass "windows prints download URL and fails"
else
  bad "windows prints download URL and fails"; cat "$work/out"
fi

if run BURZBLOG_VERSION=v9.9.9; then
  bad "missing release errors"
elif grep -q "could not download" "$work/out"; then
  pass "missing release errors"
else
  bad "missing release errors"; cat "$work/out"
fi

for v in 2.0.0 'v2.0' 'v1.0.0/../../x' 'v1.0.0;id' 'v1.0.0 x'; do
  if run "BURZBLOG_VERSION=$v"; then
    bad "bad version refused: $v"
  elif grep -q "invalid version" "$work/out" && [ ! -e "$work/bin/burzblog" ]; then
    pass "bad version refused: $v"
  else
    bad "bad version refused: $v"; cat "$work/out"
  fi
done

for b in http://example.com/releases ftp://127.0.0.1/releases http://127.0.0.1.evil.com/releases http://localhost.evil.com/releases http://127.0.0.1:80@evil.com/releases; do
  if run BURZBLOG_VERSION=v1.0.0 "BURZBLOG_BASE_URL=$b"; then
    bad "non-https base refused: $b"
  elif grep -q "BURZBLOG_BASE_URL must" "$work/out" && [ ! -e "$work/bin/burzblog" ]; then
    pass "non-https base refused: $b"
  else
    bad "non-https base refused: $b"; cat "$work/out"
  fi
done

# Install over an existing binary: new content, mode 755, no temp files left.
run BURZBLOG_VERSION=v1.0.0 || true
if env PATH="$fakebin:$PATH" BURZBLOG_BASE_URL="$base" BURZBLOG_INSTALL_DIR="$work/bin" BURZBLOG_VERSION=v2.0.0 \
  sh "$installer" >"$work/out" 2>&1 &&
  [ "$("$work/bin/burzblog")" = "burzblog v2.0.0" ] &&
  [ "$(ls -A "$work/bin")" = burzblog ] &&
  [ "$(find "$work/bin/burzblog" -perm 755)" ]; then
  pass "install over existing binary leaves no temp files"
else
  bad "install over existing binary leaves no temp files"; cat "$work/out"; ls -A "$work/bin"
fi

# A failed install (bad checksum) over an existing binary keeps the old one.
if env PATH="$fakebin:$PATH" BURZBLOG_BASE_URL="$base" BURZBLOG_INSTALL_DIR="$work/bin" BURZBLOG_VERSION=v3.0.0 \
  sh "$installer" >"$work/out" 2>&1; then
  bad "failed install keeps the old binary"
elif [ "$("$work/bin/burzblog")" = "burzblog v2.0.0" ] && [ "$(ls -A "$work/bin")" = burzblog ]; then
  pass "failed install keeps the old binary"
else
  bad "failed install keeps the old binary"; cat "$work/out"
fi

# Root never falls back to $HOME/.local/bin.
if [ -w /usr/local/bin ]; then
  echo "skip root fallback refused (/usr/local/bin is writable)"
else
  printf '#!/bin/sh\necho 0\n' >"$fakebin/id"
  chmod +x "$fakebin/id"
  if env PATH="$fakebin:$PATH" HOME="$work/home" BURZBLOG_BASE_URL="$base" BURZBLOG_VERSION=v1.0.0 \
    sh "$installer" >"$work/out" 2>&1; then
    bad "root fallback refused"
  elif grep -q "BURZBLOG_INSTALL_DIR" "$work/out" && [ ! -e "$work/home" ]; then
    pass "root fallback refused"
  else
    bad "root fallback refused"; cat "$work/out"
  fi
  rm -f "$fakebin/id"
fi

if sh "$installer" --help | grep -q "BURZBLOG_VERSION"; then
  pass "--help prints usage"
else
  bad "--help prints usage"
fi

if [ "$failures" -gt 0 ]; then
  echo "install-test: $failures failed" >&2
  exit 1
fi
echo "install-test: all passed"
