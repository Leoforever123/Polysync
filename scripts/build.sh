#!/usr/bin/env sh
set -eu

VERSION="${1:-0.2.0}"
PROJECT_ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
DIST_DIR="$PROJECT_ROOT/dist"
GOCACHE="$PROJECT_ROOT/.cache/go-build"
export GOCACHE
mkdir -p "$DIST_DIR" "$GOCACHE"

build() {
  target_os="$1"
  target_arch="$2"
  extension="$3"
  output="$DIST_DIR/polysync-$target_os-$target_arch$extension"
  echo "Building $output"
  GOOS="$target_os" GOARCH="$target_arch" go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$output" ./cmd/polysync
}

cd "$PROJECT_ROOT"
build windows amd64 .exe
build linux amd64 ""
build linux arm64 ""
build darwin amd64 ""
build darwin arm64 ""
echo "Build complete: $DIST_DIR"
