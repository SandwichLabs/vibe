#!/bin/bash
# macOS port of harness/iso-run.linux-reference.sh (bash 3.2).
# usage: iso-run-mac.sh <label> <mode>
# Starts the loopback listener, runs the harness build of Studio with SPIKE_MODE, and clicks once
# in the middle of the Studio window (harness click.swift, a real CGEvent click) for every READY_CLICK.
R=/Users/zac/html-pages-mac-qa-results-2026-10-22
L=$1; MODE=$2; OUT=$R/logs/B2-iso-$L
BIN="/Users/zac/projects/sandwich-labs/vibe/studio/build/bin/Vibe Studio.app/Contents/MacOS/vibe-studio"
: > $OUT.listener.log; : > $OUT.go.log; : > $OUT.clicks; : > $OUT.click.log
python3 -I /Users/zac/html-pages-mac-qa/harness/listener_mac.py 39547 $OUT.listener.log > $OUT.listener.stdout 2>&1 &
LPID=$!
sleep 1
cd /Users/zac
SPIKE_MODE="$MODE" SPIKE_LOG=$OUT.go.log SPIKE_WS=/Users/zac/vibe-mac-qa/ws-b "$BIN" > $OUT.studio.log 2>&1 &
SPID=$!
i=0
while [ $i -lt 150 ]; do
  i=$((i + 1))
  grep -q ALLDONE $OUT.go.log 2>/dev/null && break
  for v in $(grep -o 'READY_CLICK .*' $OUT.go.log | sed 's/READY_CLICK //' | sort -u); do
    grep -q "CLICKED $v\$" $OUT.clicks 2>/dev/null && continue
    echo "$(date +%T) $v" >> $OUT.click.log
    $R/bin/click "vibe studio" >> $OUT.click.log 2>&1; echo "CLICKED $v" >> $OUT.clicks
  done
  sleep 1
done
sleep 2
kill $SPID 2>/dev/null; sleep 1; kill $LPID 2>/dev/null
echo "finished label=$L alldone=$(grep -c ALLDONE $OUT.go.log) listener-lines=$(wc -l < $OUT.listener.log | tr -d ' ')"
