#!/usr/bin/env bash
# Fail if a Mach-O binary's minimum macOS (vtool minos) is newer than the app's LSMinimumSystemVersion.
#
#   scripts/check-macos-minos.sh <Info.plist> <binary> [binary...]
#
# macOS only (vtool, plutil). A binary linked against a newer SDK default (a plain `go build` on a
# macos-15 runner gives minos 15.0) would not start on the macOS the plist promises.
set -euo pipefail

plist="${1:?Info.plist}"
shift
[ "$#" -gt 0 ] || { echo "usage: $0 <Info.plist> <binary>..." >&2; exit 2; }

# macos_minos reads `vtool -show-build` output on stdin and prints each slice's minimum macOS.
macos_minos() {
	awk '$1 == "cmd" { cmd = $2 }
		cmd == "LC_BUILD_VERSION" && $1 == "minos" { print $2 }
		cmd == "LC_VERSION_MIN_MACOSX" && $1 == "version" { print $2 }'
}

min="$(plutil -extract LSMinimumSystemVersion raw -o - "$plist")"
status=0
for bin in "$@"; do
	# `vtool -show-build` prints "minos 14.0" under LC_BUILD_VERSION or "version 13.0" under
	# LC_VERSION_MIN_MACOSX, once per slice of a universal binary. Compare the highest. Only read
	# those keys inside their own load command: LC_BUILD_VERSION also lists its tools ("tool LD",
	# "version 1167.5"), and that linker version is not a macOS version.
	minos="$(vtool -show-build "$bin" | macos_minos | sort -t. -k1,1n -k2,2n | tail -n 1)"
	if [ -z "$minos" ]; then
		echo "FAIL $bin: no minos in vtool output" >&2
		status=1
		continue
	fi
	newest="$(printf '%s\n%s\n' "$minos" "$min" | sort -t. -k1,1n -k2,2n | tail -n 1)"
	if [ "$newest" != "$min" ]; then
		echo "FAIL $bin: minos $minos is newer than LSMinimumSystemVersion $min" >&2
		status=1
	else
		echo "ok   $bin: minos $minos <= $min"
	fi
done
exit "$status"
