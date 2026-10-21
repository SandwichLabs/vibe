#!/bin/bash
cd /Users/zac/projects/sandwich-labs/vibe/spike/pagehost/mac
flags() { case $1 in
 ctl) echo "--csp permissive --no-policy";;
 csp) echo "--csp strict --no-policy";;
 cspmeta) echo "--csp strict --via meta --no-policy";;
 pol) echo "--csp strict";;
 sbx) echo "--csp permissive --no-policy --sandbox-net";;
 prx) echo "--csp permissive --no-policy --proxy";;
 allsbx) echo "--csp strict --sandbox-net";;
 allprx) echo "--csp strict --proxy";;
 allboth) echo "--csp strict --sandbox-net --proxy";;
esac; }
for cfg in ${CFGS:-ctl csp cspmeta pol sbx prx allsbx allprx allboth}; do
  for g in main open form formget loc locreplace alink alinkname metarefresh selfnav frame title storage charts; do
    python3 parent_mac.py --name B2.$cfg.$g --group $g --ttl 8 $(flags $cfg) >/dev/null
  done
  echo "cfg $cfg done"
done
echo B2-ALLDONE
