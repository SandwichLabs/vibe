#!/usr/bin/env bash
# Regenerate every raster icon from the master SVG (studio/build/icon/assay.svg).
#
#   scripts/gen-icons.sh
#
# Needs ImageMagick (`magick`) with its SVG renderer. Writes:
#   studio/build/appicon.png                    1024x1024; Wails builds iconfile.icns (macOS) from it at `wails build`
#   studio/build/linux/icons/hicolor/<N>x<N>/apps/assay.png   N = 16 32 48 64 128 256 512
#   studio/build/linux/icons/hicolor/scalable/apps/assay.svg
# The icon name is "assay-studio" (matches Icon= in studio/build/linux/assay.desktop).
# Commit the output; the build never runs this.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
svg="$root/studio/build/icon/assay.svg"
hicolor="$root/studio/build/linux/icons/hicolor"

command -v magick >/dev/null || { echo "gen-icons: ImageMagick (magick) is required" >&2; exit 1; }

master="$(mktemp --suffix=.png)"
trap 'rm -f "$master"' EXIT
magick -background none -density 96 "$svg" -resize 1024x1024 "PNG32:$master"

render() { # $1 = size, $2 = output png (downscaled from the 1024 master)
	magick "$master" -filter Lanczos -resize "${1}x${1}" -strip \
		-define png:exclude-chunks=date,time "PNG32:$2"
}

render 1024 "$root/studio/build/appicon.png"
for n in 16 32 48 64 128 256 512; do
	mkdir -p "$hicolor/${n}x${n}/apps"
	render "$n" "$hicolor/${n}x${n}/apps/assay.png"
done
mkdir -p "$hicolor/scalable/apps"
cp "$svg" "$hicolor/scalable/apps/assay.svg"
echo "icons regenerated from $svg"
