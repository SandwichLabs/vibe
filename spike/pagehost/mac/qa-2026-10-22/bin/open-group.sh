#!/bin/bash
# open-group.sh <group> [target]: set the probe's GROUP (and TARGET) fields in Studio with real keys,
# read them back until they hold exactly what was asked (macOS may auto-capitalise), then click Open page / Reopen.
D=$(cd "$(dirname "$0")" && pwd); P=$(cat "$D/../logs/.studio-pid")
"$D/axq" front "$P" >/dev/null
setf() { # setf <label> <value>
  n=0
  while [ $n -lt 5 ]; do
    n=$((n + 1))
    "$D/axq" click "$P" "$1 |" 0 >/dev/null; "$D/axq" key 0 cmd >/dev/null; "$D/axq" key 51 >/dev/null
    "$D/axq" type "$2" >/dev/null; sleep 0.4
    got=$("$D/axq" dump "$P" 60 | grep -E "AXTextField .* $1 \| " | sed -E "s/.* $1 \| //; s/ *$//")
    [ "$got" = "$2" ] && { echo "$1=$got (try $n)"; return 0; }
    echo "$1 read back '$got', want '$2'; retrying"
  done
  echo "FAILED to set $1"; return 1
}
setf GROUP "$1" || exit 1
if [ -n "$2" ]; then setf TARGET "$2" || exit 1; fi
sleep 0.3
"$D/axq" click "$P" "Open page" 0 2>/dev/null | tail -1 | grep -q clicked || "$D/axq" click "$P" "Reopen" 0 | tail -1
