#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
#
# Downloads the original captures listed in testdata/captures/SHA256SUMS
# into the capture cache (BACNET_CAPTURE_CACHE, default
# testdata/captures/cache, git-ignored) and verifies every SHA256. The
# originals are not redistributed with this repository
# (testdata/captures/README.md). Files already cached with the right hash
# are not downloaded again.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
sums="$root/testdata/captures/SHA256SUMS"
cache="${BACNET_CAPTURE_CACHE:-$root/testdata/captures/cache}"

sha() { if command -v sha256sum >/dev/null; then sha256sum "$1"; else shasum -a 256 "$1"; fi | cut -d' ' -f1; }

fetched=0
while IFS= read -r line; do
	want=${line%%  *}
	rel=${line#*  }
	source=${rel%%/*}
	name=${rel#*/}
	case "$source" in
	kargs) url="https://kargs.net/captures/$(printf '%s' "$name" | sed -e 's/%/%25/g' -e 's/ /%20/g' -e 's/#/%23/g' -e 's/+/%2B/g')" ;;
	*) echo "unknown source $source" >&2; exit 1 ;;
	esac
	dst="$cache/$source/$name"
	if [ -f "$dst" ] && [ "$(sha "$dst")" = "$want" ]; then
		continue
	fi
	mkdir -p "$(dirname "$dst")"
	curl -fsSL --retry 3 -o "$dst.part" "$url"
	got=$(sha "$dst.part")
	if [ "$got" != "$want" ]; then
		rm -f "$dst.part"
		echo "$rel: SHA256 $got, expected $want ($url)" >&2
		exit 1
	fi
	mv "$dst.part" "$dst"
	fetched=$((fetched + 1))
done <"$sums"
echo "captures: $fetched downloaded, all $(wc -l <"$sums" | tr -d ' ') verified in $cache"
