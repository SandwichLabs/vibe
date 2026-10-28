#!/usr/bin/env bash
# SPIKE #257 - throwaway. X11 only (run Studio with mode x11): window screenshot by title via ImageMagick.
#   shot.sh <out.png> [title-substring]
set -euo pipefail
export DISPLAY=:0
title=${2:-Vibe Studio}
for id in $(xprop -root _NET_CLIENT_LIST | sed 's/.*# //; s/,//g'); do
  if xprop -id "$id" WM_NAME 2>/dev/null | grep -q "$title"; then import -window "$id" "$1"; echo "$id"; exit 0; fi
done
echo "no window titled $title" >&2; exit 1
