#!/usr/bin/env bash
# Stage and tar one platform's release archives (see docs/packaging.md, "v0.1.0 unsigned pipeline").
#
#   scripts/package.sh <os> <arch> <version> <outdir>
#
# Expects ./assay (CLI) and studio/build/bin/assay-studio (Linux) or
# studio/build/bin/assay-studio.app (macOS) to be built already. <version> has no leading "v".
# macOS: the bundle is renamed to "Assay.app" and LSMinimumSystemVersion is set for <arch>.
# Writes <outdir>/assay_<ver>_<os>_<arch>.tar.gz (CLI only) and
# <outdir>/assay-studio_<ver>_<os>_<arch>.tar.gz (CLI plus Studio).
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
	# --- notices (#40): NOTICE and the generated third-party notices ship in every archive ---
	for f in LICENSE NOTICE THIRD_PARTY_NOTICES.md; do
		[ -f "$root/$f" ] || { echo "package.sh: $f is missing" >&2; exit 1; }
		cp "$root/$f" "$1/$f"
	done
	# --- end notices ---
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
cp "$root/assay" "$cli/assay"
docs "$cli"
pack "$cli" "$out/assay_${ver}_${os}_${arch}.tar.gz"

# CLI plus Studio.
st="$stage/studio"
mkdir -p "$st"
docs "$st"
case "$os" in
linux)
	mkdir -p "$st/bin" "$st/share/applications" "$st/share/mime/packages"
	cp "$root/assay" "$st/bin/assay"
	cp "$root/studio/build/bin/assay-studio" "$st/bin/assay-studio"
	cp "$root/studio/build/linux/assay.desktop" "$st/share/applications/assay.desktop"
	cp "$root/studio/build/linux/assay-mime.xml" "$st/share/mime/packages/assay-bundle.xml"
	# Icons: the hicolor tree from scripts/gen-icons.sh (name "assay-studio", matching Icon= in the .desktop file).
	mkdir -p "$st/share/icons"
	cp -R "$root/studio/build/linux/icons/hicolor" "$st/share/icons/hicolor"
	chmod 755 "$st/bin/assay" "$st/bin/assay-studio"
	;;
darwin)
	cp -R "$root/studio/build/bin/assay-studio.app" "$st/Assay.app"
	cp "$root/assay" "$st/assay"
	# The plist template says the lowest macOS we support; the archive is per arch, so stamp this arch's
	# minimum (scripts/macos-target.sh) before signing. release.yml then checks the binaries' minos against it.
	plutil -replace LSMinimumSystemVersion -string "$("$root/scripts/macos-target.sh" "$arch")" \
		"$st/Assay.app/Contents/Info.plist"
	# Ad-hoc sign once every file is in place; nothing may touch the bundle afterwards.
	# No --options runtime: without the hardened runtime, library validation is not enforced, so
	# DuckDB extension dylibs downloaded at run time still load.
	codesign --force --deep -s - "$st/Assay.app"
	codesign --verify --deep --strict --verbose=2 "$st/Assay.app"
	;;
*)
	echo "unsupported os: $os" >&2
	exit 1
	;;
esac
pack "$st" "$out/assay-studio_${ver}_${os}_${arch}.tar.gz"
ls -l "$out"
