#!/bin/sh
# Vercel's build (server/vercel.json): the website into the server, then the server, which
# serves when Vercel starts it without a command. Vercel runs it in server/ and names the file
# the program goes to in VERCEL_OUTPUT_FILE.
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
version=$("$root/scripts/version.sh")
# Vercel builds without .git, which leaves version.sh no commit: Vercel names it.
case "$version" in
*-dev | *.dev) [ -n "${VERCEL_GIT_COMMIT_SHA:-}" ] && version="$version+$(printf '%.7s' "$VERCEL_GIT_COMMIT_SHA")" ;;
esac

echo "Building the website"
(cd "$root/web" && npm ci --no-audit --no-fund && npm run build)

echo "Building the server ($version)"
(cd "$root/server" && CGO_ENABLED=0 GOOS=linux \
	go build -trimpath -ldflags "-s -w -X main.version=$version -X main.defaultCommand=serve" -o "$VERCEL_OUTPUT_FILE" ./cmd/share)
