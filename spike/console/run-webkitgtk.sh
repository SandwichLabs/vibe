#!/usr/bin/env bash
# SPIKE #257 - throwaway. Start Studio (wails dev, or a built binary with BIN=...) for the console harness.
#   run-webkitgtk.sh <hw|hx|wayland|x11|chromium>   (hw/hx need headless-compositor.sh start; wayland/x11 use the user's session, which is locked on this host)
#   (old usage line follows)
#   run-webkitgtk.sh <wayland|x11|chromium> '<route>'
# Writes evidence to spike/console/evidence, logs to evidence/wails-dev-<ts>.log, prints the PGID
# (the whole process group is yours: stop it with `kill -- -PGID`, never pkill -f).
set -euo pipefail
mode=${1:-wayland}; route=${2:-}
here=$(cd "$(dirname "$0")" && pwd); root=$(cd "$here/../.." && pwd)
ev="$here/evidence"; mkdir -p "$ev"
ts=$(date +%H%M%S)
log="$ev/wails-${BIN:+prod-}$mode-$ts.log"
export XDG_RUNTIME_DIR=/run/user/1000 DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1000/bus
export VIBE_SPIKE_EVIDENCE_DIR="$ev"
[ -n "$route" ] && export VIBE_SPIKE_ROUTE="$route"
case "$mode" in
  wayland) export WAYLAND_DISPLAY=wayland-0 GDK_BACKEND=wayland; unset DISPLAY ;;
  x11)     export GDK_BACKEND=x11 DISPLAY=:0; unset WAYLAND_DISPLAY ;;
  hw) . "$ev/headless.env"; export DBUS_SESSION_BUS_ADDRESS=$SPIKE_DBUS WAYLAND_DISPLAY=$SPIKE_WL GDK_BACKEND=wayland; unset DISPLAY XAUTHORITY ;;  # private headless mutter, native Wayland
  hx) . "$ev/headless.env"; export DBUS_SESSION_BUS_ADDRESS=$SPIKE_DBUS GDK_BACKEND=x11 DISPLAY=$SPIKE_XDISPLAY XAUTHORITY=$SPIKE_XAUTH; unset WAYLAND_DISPLAY ;;     # same compositor's Xwayland
  chromium) export WAYLAND_DISPLAY=wayland-0 GDK_BACKEND=wayland; unset DISPLAY ;; # window opens but Rodney drives :34115
esac
if [ -n "${BIN:-}" ]; then cmd=("$BIN"); cd "$root/studio"; else cmd=(wails dev -tags webkit2_41,duckdb_arrow -loglevel Info); cd "$root/studio"; fi
setsid "${cmd[@]}" >"$log" 2>&1 &
pid=$!
echo "$pid" > "$ev/last.pid"
echo "PGID=$pid log=$log"
