# Part C section 8 listener (the checklist's inline script, as a file): TEST-NET-1 on the lo0 alias.
import socket, time
s = socket.socket(); s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1); s.bind(('192.0.2.1', 39548)); s.listen(8)
while True:
    c, a = s.accept(); c.settimeout(1.0)
    try: d = c.recv(64)
    except Exception: d = b''
    print(time.strftime('%H:%M:%S'), 'HIT', a, d[:12].hex() or '(no bytes)', flush=True); c.close()
