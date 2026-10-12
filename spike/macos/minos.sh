#!/usr/bin/env bash
# Spike #43 round 2: rebuild with MACOSX_DEPLOYMENT_TARGET=13.0, report minos and ld warnings, stage artifacts.
set -uo pipefail
ROOT="$(pwd)"; ARCH="${ARCH:?}"
R() { echo "RESULT $ARCH $1: $2"; }
export MACOSX_DEPLOYMENT_TARGET=13.0 CGO_CFLAGS="-mmacosx-version-min=13.0" CGO_LDFLAGS="-mmacosx-version-min=13.0"
mkdir -p min13
go build -ldflags "-s -w -X github.com/SandwichLabs/vibe/cmd.version=0.0.0-spike" -o min13/vibe ./cmd/vibe 2>min13/cli-build.txt; R min13_cli_build_rc "$?"
go build -o min13/probe ./spike/macosprobe 2>>min13/cli-build.txt
R min13_cli_ld_warnings "$(grep -c 'ld: warning' min13/cli-build.txt)"
for f in vibe probe; do R "min13_minos_$f" "$(vtool -show-build min13/$f | grep -E 'minos|version|sdk' | tr -s ' ' | tr '\n' ' ')"; done
(cd studio && GOTOOLCHAIN="$WAILS_GOTOOLCHAIN" wails build -tags duckdb_arrow -ldflags "-s -w -X main.version=0.0.0-spike" >../min13/studio-build.txt 2>&1); R min13_studio_build_rc "$?"
R min13_studio_ld_warnings "$(grep -c 'ld: warning' min13/studio-build.txt)"
grep 'ld: warning' min13/studio-build.txt | grep -o "built for newer[^)]*)[^)]*)" | sort | uniq -c | head
S=studio/build/bin/vibe-studio.app/Contents/MacOS/vibe-studio
R min13_studio_minos "$(vtool -show-build $S | tr -s ' ' | tr '\n' ' ')"
cp -R studio/build/bin/vibe-studio.app min13/
tar -C min13 -czf min13-$ARCH.tar.gz vibe probe vibe-studio.app
ls -l min13-$ARCH.tar.gz
