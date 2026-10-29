#!/bin/bash
# SPIKE #257 - throwaway. launch.sh APP QUERY: start one spike window without the automatic run (lbl=ax mirrors
# every received byte to ~/vibe-spike-evidence/*-ax-keys-rx.json), wait for it, bring it forward, print its pid.
here=$(cd "$(dirname "$0")" && pwd); APP="$1"; Q="$2"
rm -f ~/vibe-spike-evidence/*-ax-keys-rx.json
VIBE_SPIKE_EVIDENCE_DIR=$HOME/vibe-spike-evidence VIBE_SPIKE_ROUTE="#/spike/console?$Q&lbl=ax" nohup "$APP" >"$HOME/vibe-spike-evidence/ax-app.log" 2>&1 &
P=$!
for _ in $(seq 1 30); do sleep 1; ls ~/vibe-spike-evidence/*-ax-keys-rx.json >/dev/null 2>&1 && "$here/drive" win $P >/dev/null 2>&1 && break; done
"$here/drive" activate $P >/dev/null; echo $P
