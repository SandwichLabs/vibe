#!/usr/bin/env bash
# SPIKE #257 - throwaway. Drive a running Studio window (see run-webkitgtk.sh) through every configuration:
# writes the route to evidence/route.txt, which the Go side follows, and waits for the results file.
#   LBL=wl|x11 (optional suffix for the file tag)  webkit-matrix.sh <label: webkitgtk-dev|webkitgtk-prod> [config ...]   (config = query string, e.g. 'widget=xterm-dom')
set -u
here=$(cd "$(dirname "$0")" && pwd); ev="$here/evidence"
label=${1:?label}; shift
configs=("$@")
[ ${#configs[@]} -eq 0 ] && configs=("widget=ghostty&wasm=inline" "widget=ghostty&wasm=url" "widget=ghostty&wasm=url&sb=30000000" "widget=xterm-webgl" "widget=xterm-webgl&pdb=1" "widget=xterm-dom")
tagof() { # config -> file tag prefix (must match ConsoleSpikeView's tag)
  local c=$1 w wasm sb pdb t
  w=$(sed -n 's/.*widget=\([a-z-]*\).*/\1/p' <<<"$c"); wasm=$(sed -n 's/.*wasm=\([a-z]*\).*/\1/p' <<<"$c"); sb=$(sed -n 's/.*sb=\([0-9]*\).*/\1/p' <<<"$c"); pdb=$(grep -q 'pdb=1' <<<"$c" && echo -pdb)
  t=$w; [ "$w" = ghostty ] && { t="$t-${wasm:-inline}"; [ -n "$sb" ] && t="$t-sb$sb"; }
  echo "$t$pdb-$label${LBL:+-$LBL}"
}
for c in "${configs[@]}"; do
  tag=$(tagof "$c"); rm -f "$ev/$tag"-*
  printf '#/spike/console?%s&auto=1%s\t%s\n' "$c" "${LBL:+&lbl=$LBL}" "$RANDOM$RANDOM" > "$ev/route.txt"
  for i in $(seq 1 90); do [ -f "$ev/$tag-results.json" ] && break; sleep 2; done
  if [ -f "$ev/$tag-results.json" ]; then echo "ok   $tag"; else echo "TIMEOUT $tag"; fi
done
