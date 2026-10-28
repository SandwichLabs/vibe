#!/usr/bin/env bash
# SPIKE #257 - throwaway. Iconify the X11 window right after the automatic run starts, to see which widget's
# write callbacks and frames survive a window the compositor does not present. Needs run-webkitgtk.sh hx.
cd "$(dirname "$0")"; . evidence/headless.env; export DISPLAY=$SPIKE_XDISPLAY XAUTHORITY=$SPIKE_XAUTH
ev=$PWD/evidence
for cfg in "ghostty|widget=ghostty&wasm=url&sb=30000000" "xterm|widget=xterm-webgl"; do
  n=${cfg%%|*}; q=${cfg#*|}
  rm -f "${ev:?}"/*-hid-*
  printf '#/spike/console?%s&auto=1&lbl=hid\t%s\n' "$q" "$RANDOM" > "$ev/route.txt"
  sleep 3; python3 hide-window.py iconify
  sleep 12; echo "== $n hidden for 12 s: progress=$(tail -c 60 "$ev"/*-hid-progress.txt 2>/dev/null) | results files: $(ls "$ev"/*-hid-results.json 2>/dev/null | wc -l)"
  cat "$ev"/*-hid-raf.json 2>/dev/null | tr '\n' ' '; echo
  python3 hide-window.py show; sleep 25; echo "   after show: progress=$(tail -c 60 "$ev"/*-hid-progress.txt 2>/dev/null) | results files: $(ls "$ev"/*-hid-results.json 2>/dev/null | wc -l)"
  cp "$ev"/*-hid-progress.txt "$ev/hidden-$n-progress.txt" 2>/dev/null
  cp "$ev"/*-hid-raf.json "$ev/hidden-$n-raf.json" 2>/dev/null
done
