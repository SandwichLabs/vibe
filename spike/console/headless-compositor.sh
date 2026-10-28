#!/usr/bin/env bash
# SPIKE #257 - throwaway. The host's desktop is locked, so its compositor never presents a window (WebKit reports
# visibilityState=hidden and requestAnimationFrame never fires). This starts a PRIVATE headless mutter (own D-Bus
# session bus, own Wayland socket, own Xwayland) that touches nothing of the user's session.
#   headless-compositor.sh start   -> prints env lines (eval them); state in evidence/headless.env
#   headless-compositor.sh stop
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd); st="$here/evidence/headless.env"; run=/run/user/1000
case "${1:-}" in
  start)
    mkdir -p "$here/evidence"
    out=$(/usr/bin/env dbus-daemon --session --fork --print-address=1 --print-pid=1 2>&1)
    addr=$(sed -n 1p <<<"$out"); dpid=$(sed -n 2p <<<"$out")
    DBUS_SESSION_BUS_ADDRESS=$addr XDG_RUNTIME_DIR=$run setsid mutter --headless --wayland --wayland-display=spike-wl --virtual-monitor "${SIZE:-1400x900}" >"$here/evidence/mutter.log" 2>&1 &
    mpid=$!
    for i in $(seq 1 30); do [ -S "$run/spike-wl" ] && break; sleep 0.5; done
    [ -S "$run/spike-wl" ] || { echo "mutter did not come up; see evidence/mutter.log" >&2; kill "$mpid" "$dpid" 2>/dev/null || true; exit 1; }
    sleep 1
    xdisp=$(grep -o 'DISPLAY=:[0-9]*\|Using X11 display :[0-9]*\|:[0-9]\+' "$here/evidence/mutter.log" | head -1 || true)
    xdisp=$(sed -n 's/.*Using public X11 display \(:[0-9]*\),.*/\1/p' "$here/evidence/mutter.log" | head -1)
    xauth=$(ps -eo args | sed -n "s|^/usr/bin/Xwayland $xdisp .*-auth \([^ ]*\) .*|\1|p" | head -1)
    printf 'SPIKE_XDISPLAY=%s\nSPIKE_XAUTH=%s\n' "$xdisp" "$xauth" >> "$st.tmp"
    cat "$st.tmp" > /dev/null
    printf 'SPIKE_DBUS_PID=%s\nSPIKE_MUTTER_PID=%s\nSPIKE_DBUS=%s\nSPIKE_WL=spike-wl\n' "$dpid" "$mpid" "$addr" > "$st"
    cat "$st.tmp" >> "$st"; rm -f "$st.tmp"
    cat "$st" ;;
  stop)
    [ -f "$st" ] || exit 0
    . "$st"; kill "$SPIKE_MUTTER_PID" 2>/dev/null || true; sleep 1; kill "$SPIKE_DBUS_PID" 2>/dev/null || true; rm -f "$st" ;;
  *) echo "usage: $0 start|stop" >&2; exit 2 ;;
esac
