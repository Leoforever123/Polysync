#!/usr/bin/env sh
set -eu

VERSION="${1:-0.5.0}"
PROJECT_ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
DIST_DIR="$PROJECT_ROOT/dist"
GOCACHE="$PROJECT_ROOT/.cache/go-build"
export GOCACHE
mkdir -p "$DIST_DIR" "$GOCACHE"

checksum() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1"; else shasum -a 256 "$1"; fi
}

build() {
  target_os="$1"
  target_arch="$2"
  extension="$3"
  output="$DIST_DIR/polysync-$target_os-$target_arch$extension"
  echo "Building $output"
  flags="-s -w -X main.version=$VERSION"
  if [ "$target_os" = "windows" ]; then flags="$flags -H=windowsgui"; fi
  CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" go build -trimpath -ldflags "$flags" -o "$output" ./cmd/polysync
  if [ "$target_os" = linux ]; then
    printf '%s %s\n' "$VERSION" "$(checksum "$output" | cut -d ' ' -f 1)" > "$output.build-info"
  fi
}

cd "$PROJECT_ROOT"
build windows amd64 .exe
build linux amd64 ""
build linux arm64 ""
build darwin amd64 ""
build darwin arm64 ""
(cd "$DIST_DIR"; for asset in polysync-windows-amd64.exe polysync-linux-amd64 polysync-linux-arm64 polysync-darwin-amd64 polysync-darwin-arm64; do checksum "$asset"; done) > "$DIST_DIR/SHA256SUMS.txt"
echo "Build complete: $DIST_DIR"
