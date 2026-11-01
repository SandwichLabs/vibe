package main

// The bridge of a page window: what the page asks for, over the helper's pipe,
// and what Studio answers (docs/plans/html-artifacts.md, H4). Everything here
// is untrusted input: the helper is a process a page can compromise, and every
// line is shape-checked and size-capped by pagehost.Decode. A message never
// names an artifact (the window pins it), a query runs only as the page's own
// manifest query through the same gates as every artifact query, and an answer
// is a fixed code, never a path or a policy's reason.

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/SandwichLabs/assay/internal/engine"
	"time"

	"github.com/SandwichLabs/assay/internal/artifacts"
	"github.com/SandwichLabs/assay/internal/pagehost"
)

// serve reads the helper's messages until its pipe closes.
func (w *pageWindow) serve() {
	lr := pagehost.NewLineReader(w.conn, pagehost.FromHelper)
	for {
		line, err := lr.Next()
		if err != nil {
			w.helperGone()
			return
		}
		// Upload chunks are paced by their acknowledgements, so they have their own,
		// higher, bound; a line that claims to be one and is not ends the window below.
		var flooded bool
		if bytes.HasPrefix(line, []byte(`{"t":"put"`)) {
			now := time.Now()
			flooded = w.putFlood.HitLimit(now, pagehost.MaxPutsPerSecond) || w.putBytes.HitN(now, len(line), w.host.putBytesPerSec())
		} else {
			flooded = w.flood.Hit(time.Now())
		}
		if flooded {
			w.finish(pageStateStopped)
			return
		}
		m, err := pagehost.Decode(line, pagehost.FromHelper)
		if err != nil {
			// A line that breaks the protocol means the helper is not trusted any
			// more: the window ends.
			w.finish(pageStateStopped)
			return
		}
		w.dispatch(m)
	}
}

func (w *pageWindow) dispatch(m *pagehost.Message) {
	switch m.T {
	case pagehost.TypeHeartbeat:
		w.beat.Store(time.Now().UnixNano())
	case pagehost.TypeReady:
		w.beat.CompareAndSwap(0, time.Now().UnixNano())
		w.host.report(w, pageStateOpen)
	case pagehost.TypeRun:
		go w.handleRun(m)
	case pagehost.TypeDoc:
		go w.handleDoc(m)
	case pagehost.TypeCall:
		go w.handleCall(m)
	case pagehost.TypePut:
		w.handlePut(m)
	case pagehost.TypeGone:
		w.finish(pageStateStopped)
	case pagehost.TypeClosed:
		w.end(pageStateClosed, true)
	}
}

// fail answers a request with a fixed code.
func (w *pageWindow) fail(id int64, code string) {
	ok := false
	if err := w.out.Send(&pagehost.Message{T: pagehost.TypeReply, ID: id, OK: &ok, Code: code}); err != nil {
		w.finish(pageStateStopped)
	}
}

func (w *pageWindow) succeed(id int64, result []byte) {
	ok := true
	if err := w.out.Send(&pagehost.Message{T: pagehost.TypeReply, ID: id, OK: &ok, Result: result}); err != nil {
		w.finish(pageStateStopped)
	}
}

// handleRun answers assay.run(query, values). The query is looked up in the
// manifest the window was opened with, and runs through RunManifestQuery's
// gates: vetted SQL on every run, Bind (a value is only ever a bound
// parameter), the read-only runner, the row cap and the time limit.
func (w *pageWindow) handleRun(m *pagehost.Message) {
	if !w.limit.Acquire(time.Now()) {
		w.fail(m.ID, pagehost.CodeLimit)
		return
	}
	defer w.limit.Release()
	if !w.stillAllowed() {
		w.fail(m.ID, pagehost.CodeRefused)
		return
	}
	if !queryListed(w.page, m.Q) && w.host.logger != nil {
		// Allowed, since it is still a manifest query, but it is logged (P4 review).
		w.host.logger.Info("a page ran a query its templates do not use", "artifact", w.name, "query", m.Q)
	}
	rows, err := w.run(w.ctx, m.Q, m.V)
	if err != nil {
		if w.host.logger != nil {
			w.host.logger.Debug("page query failed", "artifact", w.name, "query", m.Q, "error", engine.ErrorForLog(err))
		}
		code := pagehost.CodeQuery
		if errors.Is(err, engine.ErrResultTooLarge) {
			code = pagehost.CodeTooLarge
		}
		w.fail(m.ID, code)
		return
	}
	if !w.stillAllowed() {
		w.fail(m.ID, pagehost.CodeRefused)
		return
	}
	out, err := encodeRows(rows)
	if err != nil {
		w.fail(m.ID, pagehost.CodeTooLarge)
		return
	}
	w.succeed(m.ID, out)
}

func queryListed(a *artifacts.Artifact, name string) bool {
	if a.Page == nil {
		return false
	}
	for _, q := range a.Page.Queries {
		if q == name {
			return true
		}
	}
	return false
}

// resultCap leaves room in a 2 MB line for the reply's envelope.
const resultCap = pagehost.MaxToHelper - 8<<10

var errResultTooLarge = errors.New("the result is too large")

func encodeRows(r *artifacts.PageRows) ([]byte, error) {
	out := struct {
		Columns   []string `json:"columns"`
		Rows      [][]any  `json:"rows"`
		Truncated bool     `json:"truncated"`
	}{Columns: r.Columns, Rows: r.Rows, Truncated: r.Truncated}
	if out.Columns == nil {
		out.Columns = []string{}
	}
	if out.Rows == nil {
		out.Rows = [][]any{}
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(out); err != nil {
		return nil, errResultTooLarge
	}
	// The reply carries the result as a JSON string literal handed to
	// JSON.parse on the page's side, which can double its size.
	if b.Len() > resultCap/2 {
		return nil, errResultTooLarge
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

// handleDoc answers a navigation within the page (a link to ?param=value): the
// entry document, rendered again for the new query string, under the same
// limits as a run.
func (w *pageWindow) handleDoc(m *pagehost.Message) {
	if !w.limit.Acquire(time.Now()) || !w.stillAllowed() {
		w.finishDoc(m.ID, 429, pageErrorDoc(&artifacts.RenderError{Kind: artifacts.RenderTooSlow}))
		return
	}
	defer w.limit.Release()
	var body bytes.Buffer
	if err := renderPageQuery(w.ctx, w.run, w.page, m.Q, &body); err != nil {
		if w.host.logger != nil {
			w.host.logger.Debug("page render failed", "artifact", w.name, "error", engine.ErrorForLog(err))
		}
		w.finishDoc(m.ID, 500, pageErrorDoc(err))
		return
	}
	if !w.stillAllowed() {
		w.finishDoc(m.ID, 404, pageErrorDoc(nil))
		return
	}
	w.finishDoc(m.ID, 200, withThemeLink(body.Bytes()))
}

func pageErrorDoc(err error) []byte { return artifacts.PageErrorHTML(err) }

func (w *pageWindow) finishDoc(id int64, status int, body []byte) {
	if len(body) > pagehost.MaxDocBytes {
		body, status = pageErrorDoc(&artifacts.RenderError{Kind: artifacts.RenderTooLarge}), 500
	}
	if err := w.send(docMessages(id, status, body)); err != nil {
		w.finish(pageStateStopped)
	}
}
