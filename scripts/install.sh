#!/bin/sh
# scripts/install.sh — forge-agnostic consumer installer for atm.
# See docs/superpowers/specs/2026-07-06-semver-build-pipeline-design.md.
set -eu

FORGE=${FORGE:-gitlab}
REPO=${REPO:-}
VERSION=${VERSION:-latest}
PREFIX=${PREFIX:-/usr/local/bin}
PORT=${PORT:-8000}
DIST_DIR=${DIST_DIR:-dist}
case "$DIST_DIR" in
  /*) ;;
  *) DIST_DIR=$PWD/$DIST_DIR ;;
esac

# Map uname to one of the 4 release targets.
map_target() {
  os=$(uname -s | tr '[:upper:]' '[:lower:]')
  arch=$(uname -m)
  case "$arch" in
    x86_64|amd64) arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
    *) echo "unsupported arch: $arch" >&2; exit 3 ;;
  esac
  case "$os" in
    linux|darwin) ;;
    *) echo "unsupported os: $os" >&2; exit 3 ;;
  esac
  printf '%s/%s' "$os" "$arch"
}

resolve_version() {
  if [ "$VERSION" = "latest" ]; then
    case "$FORGE" in
      dist) sed 's/^LATEST=//' "$DIST_DIR/LATEST" ;;
      local) curl -fsS "http://localhost:$PORT/LATEST" | sed 's/^LATEST=//' ;;
      gitlab) curl -fsS "https://gitlab.com/api/v4/projects/$(printf '%s' "$REPO" | jq -sRr @uri)/releases" | jq -r '.[0].tag_name' ;;
      github) curl -fsS "https://api.github.com/repos/$REPO/releases/latest" | jq -r '.tag_name' ;;
      *) echo "unknown FORGE: $FORGE" >&2; exit 2 ;;
    esac
  else
    printf '%s' "$VERSION"
  fi
}

download_url() {
  v=$1; pair=$2
  vstripped=$(printf '%s' "$v" | sed 's/^v//')
  os=${pair%/*}; arch=${pair#*/}
  case "$FORGE" in
    dist) printf 'file://%s/atm_%s_%s_%s.tar.gz' "$DIST_DIR" "$vstripped" "$os" "$arch" ;;
    local) printf 'http://localhost:%s/atm_%s_%s_%s.tar.gz' "$PORT" "$vstripped" "$os" "$arch" ;;
    gitlab) printf 'https://gitlab.com/api/v4/projects/$(printf %%s %s | jq -sRr @uri)/releases/%s/downloads/atm_%s_%s_%s.tar.gz' "$REPO" "$v" "$vstripped" "$os" "$arch" ;;
    github) printf 'https://github.com/%s/releases/download/%s/atm_%s_%s_%s.tar.gz' "$REPO" "$v" "$vstripped" "$os" "$arch" ;;
  esac
}

pair=$(map_target)
v=$(resolve_version)
vstripped=$(printf '%s' "$v" | sed 's/^v//')
os=${pair%/*}; arch=${pair#*/}
tb="atm_${vstripped}_${os}_${arch}.tar.gz"
url=$(download_url "$v" "$pair")

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
cd "$tmp"
echo "downloading $url"
curl -fsSLO "$url"
case "$FORGE" in
  dist) curl -fsSLO "file://$DIST_DIR/SHA256SUMS" ;;
  local) curl -fsSLO "http://localhost:$PORT/SHA256SUMS" ;;
  gitlab) curl -fsSLO "https://gitlab.com/api/v4/projects/$(printf '%s' "$REPO" | jq -sRr @uri)/releases/$v/downloads/SHA256SUMS" ;;
  github) curl -fsSLO "https://github.com/$REPO/releases/download/$v/SHA256SUMS" ;;
esac
sha256sum -c SHA256SUMS --ignore-missing 2>&1 | grep -q "OK$" || { echo "checksum failed" >&2; exit 4; }

tar xzf "$tb"
if [ "$(id -u)" = 0 ] || [ -w "$(dirname "$PREFIX")" ]; then
  mkdir -p "$PREFIX"
  install -m 0755 atm "$PREFIX/atm"
else
  sudo mkdir -p "$PREFIX"
  sudo install -m 0755 atm "$PREFIX/atm"
fi
"$PREFIX/atm" version
