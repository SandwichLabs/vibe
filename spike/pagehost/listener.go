package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

// hitLog is the ground truth: every connection or packet a listener sees.
type hitLog struct {
	mu sync.Mutex
	f  *os.File
	tag string
}

func newHitLog(path, tag string) (*hitLog, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return &hitLog{f: f, tag: tag}, nil
}

func (h *hitLog) printf(format string, a ...any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	fmt.Fprintf(h.f, "%s %s %s\n", time.Now().Format("15:04:05.000"), h.tag, fmt.Sprintf(format, a...))
}

// startListeners opens TCP and UDP on the same loopback port and logs everything.
// It returns the port. TCP: accept is logged (preconnect opens no request), then the
// request line and a few headers; any HTTP request gets a CORS-open 200.
func startListeners(addr string, h *hitLog) (string, error) {
	tl, err := net.Listen("tcp", addr)
	if err != nil {
		return "", err
	}
	_, port, _ := net.SplitHostPort(tl.Addr().String())
	host, _, _ := net.SplitHostPort(addr)
	go func() {
		for {
			c, err := tl.Accept()
			if err != nil {
				return
			}
			go serveTCP(c, h)
		}
	}()
	if ul, err := net.ListenPacket("udp", net.JoinHostPort(host, port)); err == nil {
		go func() {
			buf := make([]byte, 2048)
			for {
				n, from, err := ul.ReadFrom(buf)
				if err != nil {
					return
				}
				kind := "udp"
				if n >= 20 && binary.BigEndian.Uint32(buf[4:8]) == 0x2112A442 {
					kind = fmt.Sprintf("STUN type=0x%04x", binary.BigEndian.Uint16(buf[0:2]))
				}
				h.printf("HIT udp %s from %s len=%d", kind, from, n)
			}
		}()
	}
	return port, nil
}

func serveTCP(c net.Conn, h *hitLog) {
	defer c.Close()
	h.printf("HIT tcp-accept from %s", c.RemoteAddr())
	c.SetReadDeadline(time.Now().Add(600 * time.Millisecond))
	br := bufio.NewReader(c)
	first, err := br.ReadString('\n')
	if err != nil {
		h.printf("HIT tcp-noreq (connection with no request: preconnect-like)")
		return
	}
	first = strings.TrimSpace(first)
	hdr := map[string]string{}
	for {
		l, err := br.ReadString('\n')
		if err != nil || strings.TrimSpace(l) == "" {
			break
		}
		if k, v, ok := strings.Cut(l, ":"); ok {
			hdr[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
		}
	}
	h.printf("HIT http %q host=%q origin=%q upgrade=%q", first, hdr["host"], hdr["origin"], hdr["upgrade"])
	if hdr["upgrade"] != "" {
		return
	}
	fmt.Fprint(c, "HTTP/1.1 200 OK\r\nAccess-Control-Allow-Origin: *\r\nAccess-Control-Allow-Headers: *\r\nAccess-Control-Allow-Methods: *\r\nContent-Type: text/plain\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok")
}

// dnsServer answers every A query with 127.0.0.1 and logs each question.
func dnsServer(addr string, h *hitLog) error {
	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		return err
	}
	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			q := append([]byte(nil), buf[:n]...)
			name, qtype, end := parseQuestion(q)
			h.printf("HIT dns query name=%s qtype=%d from %s", name, qtype, from)
			resp := append([]byte(nil), q[:end]...)
			resp[2], resp[3] = 0x81, 0x80
			resp[6], resp[7] = 0, 0
			if qtype == 1 {
				resp[7] = 1
				resp = append(resp, 0xC0, 0x0C, 0, 1, 0, 1, 0, 0, 0, 0, 0, 4, 127, 0, 0, 1)
			}
			resp[8], resp[9], resp[10], resp[11] = 0, 0, 0, 0
			pc.WriteTo(resp, from)
		}
	}()
	return nil
}

func parseQuestion(b []byte) (string, uint16, int) {
	i := 12
	var parts []string
	for i < len(b) && b[i] != 0 {
		l := int(b[i])
		if i+1+l > len(b) {
			return "?", 0, len(b)
		}
		parts = append(parts, string(b[i+1:i+1+l]))
		i += 1 + l
	}
	i++
	if i+4 > len(b) {
		return strings.Join(parts, "."), 0, len(b)
	}
	return strings.Join(parts, "."), binary.BigEndian.Uint16(b[i : i+2]), i + 4
}

// fakeVarlink stands in for systemd-resolved's /run/systemd/resolve/io.systemd.Resolve:
// it logs the connection and the first request, then answers with a varlink error.
func fakeVarlink(path string, h *hitLog) error {
	l, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				h.printf("HIT varlink-connect (nss-resolve reached the host resolver socket path)")
				c.SetReadDeadline(time.Now().Add(time.Second))
				buf := make([]byte, 4096)
				n, _ := c.Read(buf)
				h.printf("HIT varlink-request %q", strings.TrimRight(string(buf[:n]), "\x00"))
				c.Write([]byte(`{"error":"io.systemd.Resolve.NoNameServers"}` + "\x00"))
			}()
		}
	}()
	return nil
}
