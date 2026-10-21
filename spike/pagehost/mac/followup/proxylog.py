import socket, sys, threading, time
out = open(sys.argv[2], 'a', buffering=1)
s = socket.socket(); s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1); s.bind(('127.0.0.1', int(sys.argv[1]))); s.listen(64)
def c(k, a):
    k.settimeout(1)
    try: d = k.recv(4096)
    except Exception: d = b''
    out.write(time.strftime('%H:%M:%S ') + 'PROXY ' + repr(d.split(b'\r\n')[0]) + '\n')
    try: k.sendall(b'HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\nConnection: close\r\n\r\n')
    except Exception: pass
    k.close()
while True:
    k, a = s.accept(); threading.Thread(target=c, args=(k, a), daemon=True).start()
