#!/bin/bash
# tab.sh cfg : one line per group: counts + /v/ paths + dns names
cd /Users/zac/projects/sandwich-labs/vibe/spike/pagehost/mac
for g in main open form formget loc locreplace alink alinkname metarefresh selfnav frame title storage charts; do
  L=logs/B2.$1.$g.listener.log; [ -f $L ] || continue
  paths=$(grep -o "HIT http '[A-Z]* /v/[^ ?]*" $L | sed 's/.*\/v\///' | sort -u | tr '\n' ' ')
  dns=$(grep 'dns query' $L | grep -v resolver.arpa | sed 's/.*name=\([^.]*\).*/\1/' | sort -u | tr '\n' ' ')
  printf '%-12s http=%-2s noreq=%s udp=%s dns=[%s] paths=[%s]\n' $g $(grep -c 'HIT http' $L) $(grep -c noreq $L) $(grep -c 'HIT udp' $L) "$dns" "$paths"
done
