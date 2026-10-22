#!/bin/bash
# Undo qa-sudo-setup.sh and confirm each change is gone (bash 3.2).
set -u
[ "$(id -u)" = 0 ] || { echo "run with sudo"; exit 1; }
if [ -f /tmp/vibe-qa-tcpdump.pid ]; then kill "$(cat /tmp/vibe-qa-tcpdump.pid)" 2>/dev/null; rm -f /tmp/vibe-qa-tcpdump.pid; fi
pkill -f "tcpdump -n -l -i any udp port 53 or udp port 5353 or udp port 5354" 2>/dev/null
rm -f /etc/resolver/spike.test
ifconfig lo0 -alias 192.0.2.1 2>/dev/null
echo "--- confirm"
[ -e /etc/resolver/spike.test ] && echo "STILL THERE: /etc/resolver/spike.test" || echo "gone: /etc/resolver/spike.test"
ls /etc/resolver 2>/dev/null
ifconfig lo0 | grep -q 192.0.2.1 && echo "STILL THERE: lo0 alias 192.0.2.1" || echo "gone: lo0 alias 192.0.2.1"
pgrep -fl 'tcpdump -n -l -i any' || echo "gone: tcpdump"
