#!/usr/bin/env python3
"""macOS stand-in for Studio (spike P1, scripted host). NOT RUN on the Linux machine that wrote it.

Starts a loopback listener (TCP+UDP, logs every accept/request/packet), spawns ./pagehost_mac with a
scrubbed environment and ONE extra fd (3, a socketpair), optionally under
  sandbox-exec -p '(version 1)(allow default)(deny network*)'
answers vibe.run with fake rows, runs a heartbeat watchdog, and snapshots `ps eww` / `lsof` for the
helper and its descendants.

  ./parent_mac.py --name L1 --csp strict --group main --ttl 10 [--no-policy] [--sandbox-net] [--watchdog]
Logs: logs/<name>.{listener,parent,helper,proc}.log
"""
import argparse, json, os, signal, socket, subprocess, sys, threading, time

ap = argparse.ArgumentParser()
ap.add_argument('--name', default='run'); ap.add_argument('--csp', default='strict')
ap.add_argument('--via', default='header'); ap.add_argument('--group', default='main')
ap.add_argument('--ttl', type=float, default=10); ap.add_argument('--no-policy', action='store_true')
ap.add_argument('--sandbox-net', action='store_true', help='wrap the helper in sandbox-exec (deny network*)')
ap.add_argument('--watchdog', action='store_true'); ap.add_argument('--proxy', action='store_true', help='helper sets WKWebsiteDataStore.proxyConfigurations to a dead loopback port'); ap.add_argument('--helper', default='./pagehost_mac')
ap.add_argument('--assets', default='./probe_assets'); ap.add_argument('--logdir', default='logs')
a = ap.parse_args()
os.makedirs(a.logdir, exist_ok=True)
def lp(s): return os.path.join(a.logdir, f'{a.name}.{s}.log')
for s in ('listener', 'parent', 'helper', 'proc'):
    try: os.remove(lp(s))
    except FileNotFoundError: pass
plog = open(lp('parent'), 'a', buffering=1); llog = open(lp('listener'), 'a', buffering=1)
def logf(m): plog.write(time.strftime('%H:%M:%S ') + m + '\n')
def hit(m): llog.write(time.strftime('%H:%M:%S ') + 'HOST HIT ' + m + '\n')

# ---- loopback listener: TCP (accept + request line) and UDP (STUN-ish), same port
tl = socket.socket(); tl.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1); tl.bind(('127.0.0.1', 0)); tl.listen(64)
port = tl.getsockname()[1]
ul = socket.socket(socket.AF_INET, socket.SOCK_DGRAM); ul.bind(('127.0.0.1', port))
def tcp_conn(c, addr):
    hit(f'tcp-accept from {addr}'); c.settimeout(0.6)
    try:
        data = c.recv(4096)
    except Exception:
        data = b''
    if not data: hit('tcp-noreq (connection with no request: preconnect-like)'); c.close(); return
    first = data.split(b'\r\n')[0].decode(errors='replace'); hit(f'http {first!r}')
    if b'pgrade: websocket' not in data and b'upgrade: websocket' not in data:
        c.sendall(b'HTTP/1.1 200 OK\r\nAccess-Control-Allow-Origin: *\r\nAccess-Control-Allow-Headers: *\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok')
    c.close()
def tcp_loop():
    while True:
        c, addr = tl.accept(); threading.Thread(target=tcp_conn, args=(c, addr), daemon=True).start()
def udp_loop():
    while True:
        d, addr = ul.recvfrom(2048); hit(f'udp from {addr} len={len(d)} first={d[:8].hex()}')
threading.Thread(target=tcp_loop, daemon=True).start(); threading.Thread(target=udp_loop, daemon=True).start()
target = f'127.0.0.1:{port}'
# ---- DNS logger: needs /etc/resolver/spike.test (nameserver 127.0.0.1 / port 5354), see P1-MAC-RUN.md
import struct
def dns_loop():
    d = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    try: d.bind(('127.0.0.1', 5354))
    except OSError as e: logf('DNS logger not started: %s' % e); return
    while True:
        q, addr = d.recvfrom(1500)
        i, parts = 12, []
        while i < len(q) and q[i]: parts.append(q[i+1:i+1+q[i]].decode(errors='replace')); i += 1 + q[i]
        qtype = struct.unpack('>H', q[i+1:i+3])[0] if i + 3 <= len(q) else 0
        hit('dns query name=%s qtype=%d' % ('.'.join(parts), qtype))
        r = bytearray(q[:i+5]); r[2:4] = b'\x81\x80'; r[6:12] = b'\0\0\0\0\0\0'
        if qtype == 1: r[7] = 1; r += b'\xc0\x0c\0\1\0\1\0\0\0\0\0\4\x7f\0\0\1'
        d.sendto(bytes(r), addr)
threading.Thread(target=dns_loop, daemon=True).start()

# ---- spawn: scrubbed env (empty + what AppKit needs: nothing is a valid first try), one extra fd
pe, ce = socket.socketpair()
env = {k: os.environ[k] for k in ("SPIKE_NODNSPF","SPIKE_XDNS","SPIKE_RULES") if k in os.environ}
cmd = [a.helper, '-csp', a.csp, '-via', a.via, '-group', a.group, '-target', target, '-ttl', str(a.ttl),
       '-policy', '0' if a.no_policy else '1', '-assets', a.assets, '-dnssuf', os.environ.get('SPIKE_DNSSUF', 'spike.test'), '-proxy', '1' if a.proxy else '0']
if a.sandbox_net:
    cmd = ['/usr/bin/sandbox-exec', '-p', '(version 1)(allow default)(deny network*)'] + cmd
logf(f'cmd={cmd} env={env} target={target}')
hl = open(lp('helper'), 'a')
os.set_inheritable(ce.fileno(), True)
cefd = ce.fileno()
def pre():
    # preexec_fn runs BEFORE close_fds, so fd 3 must also be in pass_fds or it is closed again.
    if cefd != 3:
        os.dup2(cefd, 3); os.close(cefd)
    os.set_inheritable(3, True)
proc = subprocess.Popen(cmd, env=env, stdin=subprocess.DEVNULL, stdout=hl, stderr=hl, preexec_fn=pre,
                        close_fds=True, pass_fds=(cefd, 3))
ce.close(); logf(f'helper pid={proc.pid}')
last_hb = [0.0]; killed = [False]
def reader():
    f = pe.makefile('rwb', buffering=0); buf = b''
    while True:
        d = pe.recv(65536)
        if not d: logf('protocol pipe closed'); return
        buf += d
        while b'\n' in buf:
            line, buf = buf.split(b'\n', 1); l = line.decode(errors='replace')
            if l.startswith('P '):
                try: m = json.loads(l[2:])
                except Exception: logf('unparsable ' + l); continue
                if m.get('t') == 'hb': last_hb[0] = time.time()
                elif m.get('t') == 'run':
                    logf(f"page run {m.get('name')} -> fake rows")
                    pe.sendall(('R %d {"columns":["region","total"],"rows":[["east",1],["west",2]],"truncated":false}\n' % m['id']).encode())
                elif m.get('t') == 'log': logf('PAGE ' + json.dumps(m.get('o')))
            else: logf(l)
threading.Thread(target=reader, daemon=True).start()

def descendants(root):
    out = subprocess.run(['ps', '-axo', 'pid=,ppid='], capture_output=True, text=True).stdout.split()
    pairs = list(zip(map(int, out[0::2]), map(int, out[1::2]))); kids = {root}; changed = True
    while changed:
        changed = False
        for p, pp in pairs:
            if pp in kids and p not in kids: kids.add(p); changed = True
    return sorted(kids)
def snapshot():
    with open(lp('proc'), 'w') as f:
        pids = descendants(proc.pid)
        f.write(f'# helper tree: {pids}\n')
        for p in pids:
            f.write(f'\n=== pid {p}\n')
            f.write(subprocess.run(['ps', 'eww', '-p', str(p)], capture_output=True, text=True).stdout)   # environment
            f.write(subprocess.run(['lsof', '-p', str(p)], capture_output=True, text=True).stdout)       # open fds
        # XPC web content / networking services are launched by launchd, not as children: list them too
        f.write('\n=== WebKit XPC services (not children of the helper):\n')
        f.write(subprocess.run('ps -axo pid,ppid,comm | grep -i "com.apple.WebKit"', shell=True, capture_output=True, text=True).stdout)
start = time.time(); snapped = False
while proc.poll() is None:
    time.sleep(0.1); now = time.time()
    if not snapped and now - start > a.ttl - 2.5: snapped = True; snapshot()
    if a.watchdog and last_hb[0] and now - last_hb[0] > 2 and not killed[0]:
        killed[0] = True; kids = descendants(proc.pid)
        logf(f'WATCHDOG: heartbeat missed {now-last_hb[0]:.2f}s: SIGKILL helper {proc.pid}; descendants {kids}')
        proc.kill(); proc.wait(); time.sleep(1.5)
        out = subprocess.run(['ps', '-axo', 'pid=,comm='], capture_output=True, text=True).stdout
        logf('after kill, WebKit services still running: ' + ' | '.join(l for l in out.splitlines() if 'com.apple.WebKit' in l))
        break
    if now - start > a.ttl + 4: proc.kill(); break
logf(f'helper exited rc={proc.poll()}'); time.sleep(0.3); print('done', a.name)
