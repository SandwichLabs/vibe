#!/usr/bin/env bash
# Print the minimum macOS version Vibe supports on one architecture (spike #43, docs/packaging.md).
#
#   scripts/macos-target.sh <arm64|amd64>
#
# arm64 is 14.0 because DuckDB's static libraries are built for 14.0 there; Intel is 13.0.
# release.yml uses this for MACOSX_DEPLOYMENT_TARGET, the cgo flags and LSMinimumSystemVersion.
set -euo pipefail

case "${1:?arch (arm64|amd64)}" in
arm64) echo 14.0 ;;
amd64) echo 13.0 ;;
*)
	echo "unsupported arch: $1" >&2
	exit 1
	;;
esac
