#!/usr/bin/env bash
# Stage and tar one platform's release archives (see docs/packaging.md, "v0.1.0 unsigned pipeline").
#
#   scripts/package.sh <os> <arch> <version> <outdir>
#
# Expects ./vibe (CLI) and studio/build/bin/vibe-studio (Linux) or
# studio/build/bin/vibe-studio.app (macOS) to be built already. <version> has no leading "v".
# Writes <outdir>/vibe_<ver>_<os>_<arch>.tar.gz (CLI only) and
# <outdir>/vibe-studio_<ver>_<os>_<arch>.tar.gz (CLI plus Studio).
set -euo pipefail

os="${1:?os (linux|darwin)}" arch="${2:?arch (amd64|arm64)}" ver="${3:?version}" out="${4:?outdir}"
root="$(cd "$(dirname "$0")/.." && pwd)"
mkdir -p "$out"
out="$(cd "$out" && pwd)"
stage="$(mktemp -d)"
trap 'rm -rf "$stage"' EXIT
export COPYFILE_DISABLE=1 # keep AppleDouble (._*) files out of macOS tarballs

docs() { # $1 = dir to receive the shared text files
	printf '%s\n' "$ver" >"$1/VERSION"
	local f
	for f in LICENSE THIRD_PARTY_NOTICES.md; do
		[ -f "$root/$f" ] && cp "$root/$f" "$1/$f"
	done
	[ -f "$root/README.md" ] && cp "$root/README.md" "$1/README.md"
	return 0
}

# Reproducible tarballs: sorted entries, fixed owner, mtimes at the tag commit's time, no gzip timestamp.
epoch="${SOURCE_DATE_EPOCH:-0}"
pack() { # $1 = stage dir, $2 = archive path
	(
		cd "$1"
		if [ "$(uname -s)" = Darwin ]; then
			find . -exec touch -t "$(date -u -r "$epoch" +%Y%m%d%H%M.%S)" {} +
		else
			find . -exec touch -h -d "@$epoch" {} +
		fi
		find . ! -name . | LC_ALL=C sort >../list.txt
		if tar --version 2>/dev/null | grep -q GNU; then
			tar --no-recursion --owner=0 --group=0 --numeric-owner --mtime="@$epoch" -cf - -T ../list.txt
		else
			tar --no-recursion --uid 0 --gid 0 --uname root --gname wheel -cf - -T ../list.txt
		fi | gzip -n >"$2"
		rm -f ../list.txt
	)
}

# CLI-only archive: the binary at the root.
cli="$stage/cli"
mkdir -p "$cli"
cp "$root/vibe" "$cli/vibe"
docs "$cli"
pack "$cli" "$out/vibe_${ver}_${os}_${arch}.tar.gz"

# CLI plus Studio.
st="$stage/studio"
mkdir -p "$st"
docs "$st"
case "$os" in
linux)
	mkdir -p "$st/bin" "$st/share/applications" "$st/share/mime/packages"
	cp "$root/vibe" "$st/bin/vibe"
	cp "$root/studio/build/bin/vibe-studio" "$st/bin/vibe-studio"
	cp "$root/studio/build/linux/vibe-studio.desktop" "$st/share/applications/vibe-studio.desktop"
	cp "$root/studio/build/linux/vibe-mime.xml" "$st/share/mime/packages/vibe-bundle.xml"
	# Icons: none are committed yet (appicon.png is missing, see the release audit). Ship whatever exists.
	for png in "$root"/studio/build/linux/*.png; do
		[ -f "$png" ] || continue
		mkdir -p "$st/share/icons/hicolor/512x512/apps"
		cp "$png" "$st/share/icons/hicolor/512x512/apps/vibe-studio.png"
	done
	for svg in "$root"/studio/build/linux/*.svg; do
		[ -f "$svg" ] || continue
		mkdir -p "$st/share/icons/hicolor/scalable/apps"
		cp "$svg" "$st/share/icons/hicolor/scalable/apps/vibe-studio.svg"
	done
	chmod 755 "$st/bin/vibe" "$st/bin/vibe-studio"
	;;
darwin)
	cp -R "$root/studio/build/bin/vibe-studio.app" "$st/Vibe Studio.app"
	cp "$root/vibe" "$st/vibe"
	# Ad-hoc sign once every file is in place; nothing may touch the bundle afterwards.
	# No --options runtime: without the hardened runtime, library validation is not enforced, so
	# DuckDB extension dylibs downloaded at run time still load.
	codesign --force --deep -s - "$st/Vibe Studio.app"
	codesign --verify --deep --strict --verbose=2 "$st/Vibe Studio.app"
	;;
*)
	echo "unsupported os: $os" >&2
	exit 1
	;;
esac
pack "$st" "$out/vibe-studio_${ver}_${os}_${arch}.tar.gz"
ls -l "$out"
