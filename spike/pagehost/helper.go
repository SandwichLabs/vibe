package main

/*
#cgo pkg-config: webkit2gtk-4.1 gtk+-3.0 libsoup-3.0
#include <stdlib.h>
#include "host.h"
*/
import "C"

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// helperCfg is the helper's configuration, from argv (the environment is scrubbed).
type helperCfg struct {
	csp      string // strict | permissive | none
	cspVia   string // header | meta | both
	settings bool
	policy   bool
	sandbox  bool
	group    string
	target   string
	ttl      time.Duration
	libdir   string
	inns     string // "" | probe
	dns      string // "" | udp53 | varlink
	listen   string // in-ns listener log path
}

var noCloexec bool

var (
	cfg    helperCfg
	pipe   *os.File
	pipeMu sync.Mutex
)

const strictCSP = "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self'; connect-src 'self'; worker-src blob:; frame-src 'none'; form-action 'none'; base-uri 'none'"
const permissiveCSP = "default-src * vibe-page: data: blob: 'unsafe-inline' 'unsafe-eval'"

func cspFor(c helperCfg) string {
	switch c.csp {
	case "strict":
		return strictCSP
	case "permissive":
		return permissiveCSP
	}
	return ""
}

func send(line string) {
	pipeMu.Lock()
	defer pipeMu.Unlock()
	fmt.Fprintln(pipe, strings.ReplaceAll(line, "\n", " "))
}

//export goAPIScript
func goAPIScript() *C.char { return C.CString(apiScript) }

//export goMessage
func goMessage(m *C.char) { send("P " + C.GoString(m)) }

//export goPolicyLog
func goPolicyLog(kind, uri *C.char, a, b C.int) {
	send(fmt.Sprintf("H %s %s allow=%d policy_on=%d", C.GoString(kind), C.GoString(uri), a, b))
}

//export goPage
func goPage(uri *C.char, body **C.char, n *C.int, ctype **C.char, csp **C.char, status *C.int) {
	u, err := url.Parse(C.GoString(uri))
	*status = 404
	if err != nil {
		return
	}
	data, ct := pageBytes(u)
	send(fmt.Sprintf("H scheme-request %s -> %d bytes", C.GoString(uri), len(data)))
	if data == nil {
		return
	}
	*status = 200
	*body = (*C.char)(C.CBytes(data))
	*n = C.int(len(data))
	*ctype = C.CString(ct)
	policy := cspFor(cfg)
	if cfg.cspVia == "meta" {
		policy = ""
	}
	*csp = C.CString(policy)
}

func pageBytes(u *url.URL) ([]byte, string) {
	p := u.Path
	repl := strings.NewReplacer("HTTPTARGET", "http://"+cfg.target, "TARGET", cfg.target, "DNSSUF", dnsSuffix())
	switch {
	case p == "/" || p == "":
		meta := ""
		if cs := cspFor(cfg); cs != "" && (cfg.cspVia == "meta" || cfg.cspVia == "both") {
			meta = `<meta http-equiv="Content-Security-Policy" content="` + cs + `">`
		}
		group := u.Query().Get("g")
		if group == "" {
			group = cfg.group
		}
		h := strings.Replace(indexHTML, "META", meta, 1)
		h = strings.Replace(h, "GROUP", group, -1)
		return []byte(repl.Replace(h)), "text/html; charset=utf-8"
	case p == "/probe.js":
		return []byte(repl.Replace(probeJS)), "text/javascript"
	case p == "/frame.html":
		return []byte(frameHTML), "text/html; charset=utf-8"
	case p == "/_vibe/theme.css":
		return []byte(":root{--vibe-bg:#fff}"), "text/css"
	case p == "/assets/x.json":
		return []byte(`{"ok":true}`), "application/json"
	case strings.HasPrefix(p, "/libs/"):
		name := filepath.Base(p)
		b, err := os.ReadFile(filepath.Join(cfg.libdir, name))
		if err != nil {
			return nil, ""
		}
		return b, "text/javascript"
	}
	return nil, ""
}

// runHelper never returns until the GTK loop ends.
func runHelper(c helperCfg) {
	cfg = c
	runtime.LockOSThread()
	pipe = os.NewFile(3, "protocol")
	if os.Getenv("NO_CLOEXEC") == "" && len(os.Args) > 0 && !noCloexec {
		syscall.CloseOnExec(3) // WebKit spawns its processes with every inherited fd: keep the protocol pipe out of them
	}
	if c.inns != "" {
		if err := setupInNS(c); err != nil {
			fmt.Fprintln(os.Stderr, "setupInNS:", err)
			os.Exit(3)
		}
	}
	go readPipe()
	if c.ttl > 0 {
		go func() { time.Sleep(c.ttl); send("H ttl-expired"); C.host_quit() }()
	}
	start := C.CString("vibe-page://app/?g=" + c.group)
	defer C.free(unsafe.Pointer(start))
	title := C.CString("Spike page host")
	b2i := func(b bool) C.int {
		if b {
			return 1
		}
		return 0
	}
	C.host_run(b2i(c.settings), b2i(c.policy), b2i(c.sandbox), start, title)
}

func readPipe() {
	sc := bufio.NewScanner(pipe)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		l := sc.Text()
		switch {
		case strings.HasPrefix(l, "R "): // R <id> <json>
			parts := strings.SplitN(l[2:], " ", 2)
			js := fmt.Sprintf("window.__vibe_reply(%s, %s)", parts[0], parts[1])
			cs := C.CString(js)
			C.host_eval(cs)
			C.free(unsafe.Pointer(cs))
		case l == "Q":
			C.host_quit()
		}
	}
	// the protocol pipe closed: the parent is gone
	C.host_quit()
}

func dnsSuffix() string {
	if cfg.inns == "probe" {
		return "spike.test"
	}
	return "localhost"
}
