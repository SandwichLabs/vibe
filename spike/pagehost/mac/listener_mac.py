#!/usr/bin/env python3
"""Loopback ground-truth listener for the static-host half on macOS (same log format as the Linux one).
usage: ./listener_mac.py 39547 logs/A.listener.log
Logs every TCP accept, the request line of every HTTP request, and every UDP datagram. Replies 200 + CORS."""
import socket, sys, threading, time
port = int(sys.argv[1]); out = open(sys.argv[2], 'a', buffering=1)
def hit(m): out.write(time.strftime('%H:%M:%S ') + 'HOST HIT ' + m + '\n')
tl = socket.socket(); tl.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1); tl.bind(('127.0.0.1', port)); tl.listen(64)
ul = socket.socket(socket.AF_INET, socket.SOCK_DGRAM); ul.bind(('127.0.0.1', port))
def conn(c, a):
    hit(f'tcp-accept from {a}'); c.settimeout(0.6)
    try: d = c.recv(4096)
    except Exception: d = b''
    if not d: hit('tcp-noreq (connection with no request: preconnect-like)'); c.close(); return
    first = d.split(b'\r\n')[0].decode(errors='replace')
    hit('http %r' % first)
    if b'pgrade: websocket' not in d and b'upgrade: websocket' not in d:
        c.sendall(b'HTTP/1.1 200 OK\r\nAccess-Control-Allow-Origin: *\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok')
    c.close()
def udp():
    while True:
        d, a = ul.recvfrom(2048); hit(f'udp from {a} len={len(d)} first={d[:8].hex()}')
threading.Thread(target=udp, daemon=True).start()
import struct
def dns():
    d = socket.socket(socket.AF_INET, socket.SOCK_DGRAM); d.bind(('127.0.0.1', 5354))
    while True:
        q, addr = d.recvfrom(1500); i, parts = 12, []
        while i < len(q) and q[i]: parts.append(q[i+1:i+1+q[i]].decode(errors='replace')); i += 1 + q[i]
        qt = struct.unpack('>H', q[i+1:i+3])[0] if i + 3 <= len(q) else 0
        hit('dns query name=%s qtype=%d' % ('.'.join(parts), qt))
        r = bytearray(q[:i+5]); r[2:4] = b'\x81\x80'; r[6:12] = b'\0\0\0\0\0\0'
        if qt == 1: r[7] = 1; r += b'\xc0\x0c\0\1\0\1\0\0\0\0\0\4\x7f\0\0\1'
        d.sendto(bytes(r), addr)
try: threading.Thread(target=dns, daemon=True).start()
except Exception: pass
print('listening', port, flush=True)
while True:
    c, a = tl.accept(); threading.Thread(target=conn, args=(c, a), daemon=True).start()
