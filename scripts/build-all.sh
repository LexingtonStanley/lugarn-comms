#!/bin/sh
set -eu

cd "$(CDPATH= cd "$(dirname "$0")/.." && pwd)"

VERSION=${VERSION:-dev}
COMMIT=${COMMIT:-$(git rev-parse HEAD)}
version_package=github.com/LexingtonStanley/lugarn-comms/internal/version

for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do
    target_os=${target%/*}
    target_arch=${target#*/}
    output_dir="dist/lugarn-comms_${VERSION}_${target_os}_${target_arch}"
    binary=lugarn-comms
    if [ "$target_os" = windows ]; then
        binary=lugarn-comms.exe
    fi
    mkdir -p "$output_dir"
    printf 'Building %s -> %s/%s\n' "$target" "$output_dir" "$binary"
    CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" go build \
        -trimpath \
        -ldflags "-s -w -X ${version_package}.Version=${VERSION} -X ${version_package}.Commit=${COMMIT}" \
        -o "$output_dir/$binary" ./cmd/lugarn-comms
done
