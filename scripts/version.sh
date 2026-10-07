#!/bin/sh
# Share's version: the server, its website and the app have one, the one in VERSION, e.g. 0.1.0
# or 0.2.0-rc1. A release is tagged with it, v0.1.0.
#
#   scripts/version.sh            this build's version: VERSION for the commit tagged with it,
#                                 else what it was made from, 0.1.0-dev+abc1234 (.dirty when
#                                 tracked files changed)
#   scripts/version.sh code       the app's versionCode, which grows with the version: 0.1.0 is
#                                 100, 1.2.3 is 10203
#   scripts/version.sh set 0.2.0  makes 0.2.0 the version, in VERSION and app/pubspec.yaml
#
# SHARE_COMMIT names the commit for a checkout that isn't it: CI builds a pull request as a merge
# with its base, and names the branch's own commit instead.
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)

# valid stops for anything but X.Y.Z, with an optional -suffix; minor and patch stay below 100
# so that the versionCode keeps growing.
valid() {
	if ! echo "$1" | grep -Eq '^(0|[1-9][0-9]*)\.([0-9]|[1-9][0-9])\.([0-9]|[1-9][0-9])(-[0-9A-Za-z.-]+)?$'; then
		echo "version.sh: '$1' isn't a version like 0.1.0 or 0.2.0-rc1, with minor and patch below 100" >&2
		exit 1
	fi
}

# code turns X.Y.Z into X*10000 + Y*100 + Z.
code() {
	echo "${1%%-*}" | awk -F. '{ print $1 * 10000 + $2 * 100 + $3 }'
}

case "${1:-}" in
"")
	v=$(cat "$root/VERSION")
	valid "$v"
	# A build of anything else is a pre-release: 0.1.0-dev, or 0.2.0-rc1.dev for a pre-release.
	case "$v" in *-*) dev=$v.dev ;; *) dev=$v-dev ;; esac
	if ! commit=$(git -C "$root" rev-parse --short=7 HEAD 2>/dev/null); then
		echo "$dev" # not from a git checkout
		exit 0
	fi
	tag=$(git -C "$root" tag --points-at HEAD 'v*' | head -n 1)
	if [ -n "$tag" ] && [ "$tag" != "v$v" ]; then
		echo "version.sh: this commit is tagged $tag, but VERSION says $v" >&2
		exit 1
	fi
	dirty=
	git -C "$root" diff --quiet HEAD -- || dirty=.dirty
	if [ -n "$tag" ] && [ -z "$dirty" ]; then
		echo "$v"
	else
		echo "$dev+$(printf '%.7s' "${SHARE_COMMIT:-$commit}")$dirty"
	fi
	;;
code)
	v=$(cat "$root/VERSION")
	valid "$v"
	code "$v"
	;;
set)
	valid "${2:-}"
	printf '%s\n' "$2" > "$root/VERSION"
	sed "s/^version: .*/version: $2+$(code "$2")/" "$root/app/pubspec.yaml" > "$root/app/pubspec.yaml.new"
	mv "$root/app/pubspec.yaml.new" "$root/app/pubspec.yaml"
	echo "Share $2, the app's versionCode $(code "$2"): commit, then tag v$2 to release it"
	;;
*)
	echo "usage: scripts/version.sh [code | set X.Y.Z]" >&2
	exit 2
	;;
esac
