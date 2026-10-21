#!/bin/bash
# usage: arun.sh <run-name> <index-variant: none|a1|narrow> <SPIKE_MODE> [extra env assignments...]
set -u
R=/Users/zac/projects/sandwich-labs/vibe; M=$R/spike/pagehost/mac; S=$(dirname "$0")
name=$1; idx=$2; mode=$3; shift 3
mkdir -p $M/logs/A
cp $M/index.$idx.html $R/studio/frontend/index.html
LOG=$M/logs/A/$name.spike.log; LL=$M/logs/A/$name.listener.log; DEV=$M/logs/A/$name.dev.log
rm -f $LOG $LL $DEV
python3 $M/listener_mac.py 39547 $LL > /dev/null 2>&1 & LP=$!
sleep 0.5
cd $R/studio
env "$@" SPIKE_MODE="$mode" SPIKE_LOG=$LOG wails dev -tags duckdb_arrow -noreload > $DEV 2>&1 & WP=$!
seen=0; t0=$(date +%s)
while true; do
  sleep 0.5
  if [ -f $LOG ]; then
    n=$(grep -c READY_CLICK $LOG)
    if [ "$n" -gt "$seen" ]; then seen=$n; sleep 1; $S/clicker/click vibe >> $LOG.clicks 2>&1; fi
    grep -q ALLDONE $LOG && break
  fi
  [ $(( $(date +%s) - t0 )) -gt ${ATIMEOUT:-600} ] && { echo TIMEOUT >> $LOG; break; }
done
sleep 1
pkill -P $WP 2>/dev/null; kill $WP 2>/dev/null; pkill -f 'build/bin/vibe-studio' 2>/dev/null; pkill -f 'vibe-studio.app' 2>/dev/null
sleep 2; kill $LP 2>/dev/null
echo "A run $name done"
