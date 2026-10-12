#!/usr/bin/env bash
# Spike #43 round 2: run the min-13 build on an older macOS runner.
set -uo pipefail
R() { echo "RESULT old-os $1: $2"; }
to() { perl -e 'alarm shift; exec @ARGV' "$@"; }
sw_vers; uname -m; R os "$(sw_vers -productVersion) $(uname -m)"
W="$RUNNER_TEMP/old"; mkdir -p "$W/x"; tar -xzf min13-arm64.tar.gz -C "$W/x"
# Download via the browser-less path: no quarantine from tar. Ad-hoc sign like package.sh.
codesign --force --deep -s - "$W/x/vibe-studio.app"; codesign --force -s - "$W/x/vibe" "$W/x/probe"
"$W/x/vibe" --version; R cli_version_rc "$?"
"$W/x/vibe-studio.app/Contents/MacOS/vibe-studio" --version; R studio_version_rc "$?"
mkdir -p "$W/ws" && (cd "$W/ws" && "$W/x/vibe" workspace init . --name s </dev/null && "$W/x/vibe" query "select 42 as answer" </dev/null); R cli_query_rc "$?"
printf 'a,b\n1,2\n' >"$W/t.csv"
python3 -m http.server 8766 --bind 127.0.0.1 --directory "$W" >/dev/null 2>&1 & HP=$!; sleep 2
"$W/x/probe" ext "$W/ext" "http://127.0.0.1:8766/t.csv" 2>&1 | tee "$W/ext.txt"; R ext_rc "${PIPESTATUS[0]} :: $(tr '\n' ' ' <"$W/ext.txt")"
kill $HP
mkdir -p "$W/ws2"; : >"$W/err"
open -a "$W/x/vibe-studio.app" --stderr "$W/err" "$W/ws2"; sleep 20
R studio_running "$(pgrep -f 'vibe-studio.app/Contents/MacOS/vibe-studio' | wc -l | tr -d ' ')"
R onfileopen "$(grep -c SPIKE-OnFileOpen "$W/err")"
screencapture -x "$W/shot-old.png"; cp "$W/shot-old.png" . 2>/dev/null
cat "$W/err" | head -20
