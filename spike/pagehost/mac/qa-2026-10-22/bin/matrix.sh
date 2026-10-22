#!/bin/bash
# matrix.sh <label> <target> <group>...: one page window per group, a fresh loopback listener log per group.
# Writes logs/C2-<label>-<group>.{listener.log,page.txt,meta.txt}. Studio must be on the probe page (Allowed).
D=$(cd "$(dirname "$0")" && pwd); L=$D/../logs; LABEL=$1; TARGET=$2; shift 2
P=$(cat "$L/.studio-pid")
for g in "$@"; do
  O=$L/C2-$LABEL-$g
  : > $O.listener.log
  python3 -I /Users/zac/html-pages-mac-qa/harness/listener_mac.py 39547 $O.listener.log > /dev/null 2>&1 &
  LP=$!; sleep 0.7
  echo "start $(date +%T)" > $O.meta.txt
  "$D/open-group.sh" "$g" "$TARGET" >> $O.meta.txt
  sleep 2
  H=$(pgrep -f -- '--view-host' | head -1); echo "helper=$H" >> $O.meta.txt
  if [ "$g" = freeze ]; then sleep 22; else sleep 11; fi
  [ -n "$H" ] && "$D/axq" dump "$H" 4 >/dev/null && sleep 1 && AXFULL=1 "$D/axq" dump "$H" 40 | grep -v '^ *AXGroup ([^)]*) *$' | sed 's/⏎/\n      /g' > $O.page.txt
  ps -axo pid,ppid,command | grep -E -- '--view-host' | grep -v grep >> $O.meta.txt
  "$D/axq" front "$P" >/dev/null
  "$D/axq" dump "$P" 60 | grep -E 'stopped|open in its own|Reopen|Open page|Close window' >> $O.meta.txt
  "$D/axq" click "$P" 'Close window' 0 >/dev/null 2>&1; sleep 1.5
  echo "end $(date +%T)" >> $O.meta.txt
  kill $LP 2>/dev/null; wait $LP 2>/dev/null
  echo "$g: listener lines=$(wc -l < $O.listener.log | tr -d ' ') page-lines=$(wc -l < $O.page.txt 2>/dev/null | tr -d ' ')"
done
