// SPIKE #257 - throwaway, never merge. Evidence sink and self-navigation for the data console R0 harness.
// Both are inert unless VIBE_SPIKE_EVIDENCE_DIR / VIBE_SPIKE_ROUTE are set.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

var spikeName = regexp.MustCompile(`^[A-Za-z0-9._-]{1,120}$`)

// spikeMiddleware accepts POST /spike/evidence/<name> and writes the body under
// VIBE_SPIKE_EVIDENCE_DIR (a data:image/png body is decoded to a PNG file).
func spikeMiddleware(next http.Handler) http.Handler {
	dir := os.Getenv("VIBE_SPIKE_EVIDENCE_DIR")
	if dir == "" {
		return next
	}
	_ = os.MkdirAll(dir, 0o755)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "/spike/evidence/"
		if r.Method != http.MethodPost || !strings.HasPrefix(r.URL.Path, prefix) {
			next.ServeHTTP(w, r)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, prefix)
		if !spikeName.MatchString(name) {
			http.Error(w, "bad name", http.StatusBadRequest)
			return
		}
		const maxBody = 8 << 20
		body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
		if err != nil {
			http.Error(w, "read", http.StatusBadRequest)
			return
		}
		if len(body) > maxBody {
			http.Error(w, "too large", http.StatusRequestEntityTooLarge)
			return
		}
		if rest, ok := strings.CutPrefix(string(body), "data:image/png;base64,"); ok {
			if body, err = base64.StdEncoding.DecodeString(rest); err != nil {
				http.Error(w, "decode", http.StatusBadRequest)
				return
			}
		}
		// Only evidence extensions, and never an existing executable: the dir is shared with scripts.
		switch strings.ToLower(filepath.Ext(name)) {
		case ".json", ".txt", ".png":
		default:
			http.Error(w, "extension", http.StatusBadRequest)
			return
		}
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
			http.Error(w, "write", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// spikeDomReady sends the window to the harness route when VIBE_SPIKE_ROUTE is set, and
// then follows route.txt in the evidence dir (one line, a hash route) so one launch can run
// every configuration: each change navigates and reloads the page.
func spikeDomReady(ctx context.Context) {
	nav := func(route string) {
		q, _ := json.Marshal(route)
		runtime.WindowExecJS(ctx, "location.hash = "+string(q)+"; location.reload()")
	}
	if route := os.Getenv("VIBE_SPIKE_ROUTE"); route != "" && !spikeRouted.Swap(true) {
		q, _ := json.Marshal(route)
		runtime.WindowExecJS(ctx, "location.hash = "+string(q))
	}
	dir := os.Getenv("VIBE_SPIKE_EVIDENCE_DIR")
	if dir == "" || spikeFollowing.Swap(true) {
		return
	}
	go func() {
		last := ""
		for {
			time.Sleep(500 * time.Millisecond)
			b, err := os.ReadFile(filepath.Join(dir, "route.txt"))
			if err != nil {
				continue
			}
			if cur := strings.TrimSpace(string(b)); cur != "" && cur != last {
				last = cur
				nav(strings.SplitN(cur, "\t", 2)[0]) // "<route>\t<nonce>"
			}
		}
	}()
}

var spikeFollowing, spikeRouted atomic.Bool
