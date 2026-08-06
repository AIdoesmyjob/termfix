#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${1:-}"

if [[ ! "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]]; then
  echo "Usage: $0 vMAJOR.MINOR.PATCH" >&2
  exit 2
fi

cd "$ROOT_DIR"
scripts/check-local.sh
rm -rf dist-local
mkdir -p dist-local

build_target() {
  local goos="$1"
  local goarch="$2"
  local suffix="$3"
  echo "[local-release] Building ${goos}/${goarch}..."
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build \
    -ldflags "-X 'github.com/AIdoesmyjob/termfix/internal/version.Version=${VERSION}'" \
    -o "dist-local/termfix-${goos}-${goarch}${suffix}" .
}

build_target linux amd64 ""
build_target darwin amd64 ""
build_target darwin arm64 ""
build_target windows amd64 ".exe"

(
  cd dist-local
  sha256sum termfix-* >SHA256SUMS
)
echo "[local-release] Local artifacts are in $ROOT_DIR/dist-local"
