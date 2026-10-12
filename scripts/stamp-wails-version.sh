#!/usr/bin/env bash
# Run a command with studio/wails.json's info.productVersion set to <version>, then put the file back.
#
#   scripts/stamp-wails-version.sh <version> <command> [args...]
#
# Wails copies productVersion into CFBundleVersion / CFBundleShortVersionString (macOS) at
# `wails build`. The committed wails.json keeps its placeholder, so a local build never leaves the
# tree dirty: the original is restored when the command ends, however it ends. A leading "v" on
# <version> is dropped. Release CI passes ${GITHUB_REF_NAME#v}; Taskfile passes `git describe`.
set -euo pipefail

ver="${1:?version}"
shift
[ "$#" -gt 0 ] || { echo "usage: $0 <version> <command> [args...]" >&2; exit 2; }
ver="${ver#v}"

root="$(cd "$(dirname "$0")/.." && pwd)"
cfg="$root/studio/wails.json"
backup="$(mktemp)"
tmp="$(mktemp)"
cp "$cfg" "$backup"
restore() { cp "$backup" "$cfg"; rm -f "$backup" "$tmp"; }
trap restore EXIT

jq --arg v "$ver" '.info.productVersion = $v' "$cfg" >"$tmp"
cp "$tmp" "$cfg"
"$@"
