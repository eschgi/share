#!/bin/sh
# Builds the website into the server, then the server for Linux on arm64 (e.g. a Raspberry Pi
# or an ARM server) and on amd64. Output: dist/share-linux-arm64, dist/share-linux-amd64.
#
#   scripts/build-linux.sh            version from git (e.g. v0.1.0-3-gabc1234, or the commit)
#   VERSION=0.1.0 scripts/build-linux.sh
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
version=${VERSION:-$(git -C "$root" describe --tags --always --dirty 2>/dev/null || echo dev)}

echo "Building the website"
(cd "$root/web" && npm ci --no-audit --no-fund && npm run build)

mkdir -p "$root/dist"
for arch in arm64 amd64; do
	out="$root/dist/share-linux-$arch"
	echo "Building $out ($version)"
	(cd "$root/server" && CGO_ENABLED=0 GOOS=linux GOARCH=$arch \
		go build -trimpath -ldflags "-s -w -X main.version=$version" -o "$out" ./cmd/share)
done

(cd "$root/dist" && sha256sum share-* > SHA256SUMS)
echo "Done:"
ls -l "$root/dist"
