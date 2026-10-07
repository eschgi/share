#!/bin/sh
# Builds the website into the server, then the server for Windows PCs (windows/amd64) and
# Windows on ARM (windows/arm64). Output: dist/share-windows-amd64.exe,
# dist/share-windows-arm64.exe.
#
#   scripts/build-windows.sh            version from VERSION and git (scripts/version.sh)
#   VERSION=0.1.0 scripts/build-windows.sh
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
version=${VERSION:-$("$root/scripts/version.sh")}

echo "Building the website"
(cd "$root/web" && npm ci --no-audit --no-fund && npm run build)

mkdir -p "$root/dist"
for arch in amd64 arm64; do
	out="$root/dist/share-windows-$arch.exe"
	echo "Building $out ($version)"
	(cd "$root/server" && CGO_ENABLED=0 GOOS=windows GOARCH=$arch \
		go build -trimpath -ldflags "-s -w -X main.version=$version" -o "$out" ./cmd/share)
done

(cd "$root/dist" && sha256sum share-* > SHA256SUMS)
echo "Done:"
ls -l "$root/dist"
