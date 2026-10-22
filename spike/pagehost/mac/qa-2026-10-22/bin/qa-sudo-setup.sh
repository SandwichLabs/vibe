#!/bin/bash
# Root setup for the Mac page-window QA (bash 3.2). Undo with qa-sudo-teardown.sh.
#  1. /etc/resolver/spike.test -> 127.0.0.1 port 5354 (the loopback listener's fake DNS), so any
#     lookup of a *.spike.test probe name is logged by the listener as well as by tcpdump.
#  2. lo0 alias 192.0.2.1 (RFC 5737 TEST-NET-1) for Part C section 8; no packet leaves the machine.
#  3. tcpdump on loopback and all interfaces for udp 53, 5353 and 5354, in the background.
set -u
[ "$(id -u)" = 0 ] || { echo "run with sudo"; exit 1; }
LOGS=/Users/zac/html-pages-mac-qa-results-2026-10-22/logs
mkdir -p /etc/resolver
if [ -e /etc/resolver/spike.test ]; then echo "/etc/resolver/spike.test already exists; not touching it"; else
  printf 'nameserver 127.0.0.1\nport 5354\n' > /etc/resolver/spike.test && echo "added /etc/resolver/spike.test"; fi
ifconfig lo0 alias 192.0.2.1 255.255.255.255 && echo "added lo0 alias 192.0.2.1"
nohup tcpdump -n -l -i any 'udp port 53 or udp port 5353 or udp port 5354' > "$LOGS/C-dns-tcpdump.txt" 2> "$LOGS/C-dns-tcpdump.stderr" &
echo $! > /tmp/vibe-qa-tcpdump.pid
sleep 1
chown zac "$LOGS/C-dns-tcpdump.txt" "$LOGS/C-dns-tcpdump.stderr"
echo "tcpdump pid $(cat /tmp/vibe-qa-tcpdump.pid)"; ps -p "$(cat /tmp/vibe-qa-tcpdump.pid)" -o pid=,command=
scutil --dns | grep -A3 'spike.test' | head -4
ifconfig lo0 | grep 192.0.2.1
