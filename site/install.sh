#!/bin/sh
# BurzBlog installer: https://burzblog.weburz.com/install.sh
#
#   curl -fsSL https://burzblog.weburz.com/install.sh | sh
#
# Environment:
#   BURZBLOG_VERSION      release tag to install, for example v2.0.0 (default: latest)
#   BURZBLOG_INSTALL_DIR  directory to install into
#   BURZBLOG_BASE_URL     releases URL (default: https://github.com/weburz/burzblog/releases)

set -eu

usage() {
  cat <<'USAGE'
Install the burzblog binary.

Usage: install.sh [--help]

Environment:
  BURZBLOG_VERSION      Release tag to install, for example v2.0.0.
                        Default: the latest release.
  BURZBLOG_INSTALL_DIR  Directory to install into. Default: /usr/local/bin if
                        writable, otherwise $HOME/.local/bin. sudo is never
                        called for you.

The binary is checked against the release's SHA256SUMS file before it is
installed. Supported: Linux and macOS on x86_64, arm64 and 32-bit ARM.
USAGE
}

say() {
  printf '%s\n' "$*"
}

fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

have() {
  command -v "$1" >/dev/null 2>&1
}

# fetch URL DEST downloads a file with curl or wget.
fetch() {
  if have curl; then
    curl -fsSL -o "$2" "$1"
  else
    wget -q -O "$2" "$1"
  fi
}

# latest_tag prints the tag the releases/latest redirect points at.
latest_tag() {
  if have curl; then
    headers=$(curl -fsSI "$1/latest") || return 1
  else
    headers=$(wget -S --max-redirect=0 -O /dev/null "$1/latest" 2>&1) || true
  fi
  printf '%s\n' "$headers" | tr -d '\r' |
    sed -n 's|^[[:space:]]*[Ll]ocation:.*/tag/\([^/[:space:]]*\)$|\1|p' | tail -n 1
}

sha256_of() {
  if have sha256sum; then
    sha256sum "$1" | cut -d ' ' -f 1
  elif have shasum; then
    shasum -a 256 "$1" | cut -d ' ' -f 1
  else
    fail "need sha256sum or shasum to verify the download"
  fi
}

main() {
  case "${1:-}" in
    -h | --help)
      usage
      return 0
      ;;
    "") ;;
    *)
      usage >&2
      return 2
      ;;
  esac

  base=${BURZBLOG_BASE_URL:-https://github.com/weburz/burzblog/releases}
  base=${base%/}

  os=$(uname -s | tr '[:upper:]' '[:lower:]')
  case "$os" in
    linux | darwin) ;;
    mingw* | msys* | cygwin*)
      say "Windows is not supported by this script."
      say "Download burzblog-<version>-windows-amd64.exe from:"
      say "  $base/latest"
      return 1
      ;;
    *) fail "unsupported operating system: $os" ;;
  esac

  machine=$(uname -m)
  case "$machine" in
    x86_64 | amd64) arch=amd64 ;;
    aarch64 | arm64) arch=arm64 ;;
    armv7l | armv6l) arch=arm ;;
    *) fail "unsupported architecture: $machine" ;;
  esac
  if [ "$os" = darwin ] && [ "$arch" = arm ]; then
    fail "unsupported architecture on macOS: $machine"
  fi

  if ! have curl && ! have wget; then
    fail "need curl or wget"
  fi

  version=${BURZBLOG_VERSION:-}
  if [ -z "$version" ]; then
    version=$(latest_tag "$base") || version=
    [ -n "$version" ] || fail "could not find the latest release at $base/latest (is the repository public?)"
  fi

  asset="burzblog-$version-$os-$arch"
  sums="SHA256SUMS-$version"

  tmp=$(mktemp -d "${TMPDIR:-/tmp}/burzblog-install.XXXXXX")
  trap 'rm -rf "$tmp"' EXIT INT TERM HUP

  say "Installing burzblog $version ($os/$arch)"
  fetch "$base/download/$version/$asset" "$tmp/$asset" || fail "could not download $base/download/$version/$asset"
  fetch "$base/download/$version/$sums" "$tmp/$sums" || fail "could not download $base/download/$version/$sums"

  want=$(awk -v f="$asset" '$2 == f || $2 == "*" f { print $1; exit }' "$tmp/$sums")
  [ -n "$want" ] || fail "$asset is not listed in $sums"
  got=$(sha256_of "$tmp/$asset")
  if [ "$got" != "$want" ]; then
    fail "checksum mismatch for $asset (expected $want, got $got); nothing was installed"
  fi
  say "Checksum verified"

  if [ -n "${BURZBLOG_INSTALL_DIR:-}" ]; then
    dir=$BURZBLOG_INSTALL_DIR
  elif [ -w /usr/local/bin ]; then
    dir=/usr/local/bin
  else
    dir=$HOME/.local/bin
  fi
  mkdir -p "$dir" 2>/dev/null || true
  if [ ! -d "$dir" ] || [ ! -w "$dir" ]; then
    chmod +x "$tmp/$asset"
    say "$dir is not writable. Install the verified download with:"
    say "  sudo install -m 755 \"$tmp/$asset\" \"$dir/burzblog\""
    say "or rerun with BURZBLOG_INSTALL_DIR=\$HOME/.local/bin"
    # Keep the verified download so the command above still works.
    trap - EXIT INT TERM HUP
    return 1
  fi

  chmod +x "$tmp/$asset"
  mv -f "$tmp/$asset" "$dir/burzblog"
  if [ "$os" = darwin ] && have xattr; then
    xattr -d com.apple.quarantine "$dir/burzblog" 2>/dev/null || true
  fi
  say "Installed $dir/burzblog"

  case ":$PATH:" in
    *":$dir:"*) ;;
    *) say "Note: $dir is not on your PATH. Add it, for example: export PATH=\"$dir:\$PATH\"" ;;
  esac

  "$dir/burzblog" --version || true
  say ""
  say "Next: burzblog login https://your-server"
}

main "$@"
