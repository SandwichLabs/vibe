#!/bin/bash
# usage: sum.sh name...  (run from spike/pagehost/mac)
for n in "$@"; do
  L=logs/$n.listener.log; P=logs/$n.parent.log
  echo "=== $n: http=$(grep -c 'HIT http' $L) dns=$(grep -v resolver.arpa $L | grep -c 'dns query') udp=$(grep -c 'HIT udp' $L) tcp-accept=$(grep -c tcp-accept $L) noreq=$(grep -c noreq $L) PAGE=$(grep -c PAGE $P)"
  grep -E 'dns query|HIT http|HIT udp' $L | grep -v resolver.arpa | sed 's/^/   L /'
done
