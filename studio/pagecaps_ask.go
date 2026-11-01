package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/SandwichLabs/slaw/internal/pagehost"
)

// Limits of ask (H5).
const (
	maxAskPrompt = 32 << 10
	maxAskSystem = 8 << 10
	askTimeout   = 90 * time.Second
)

type askArg struct {
	Prompt string `json:"prompt"`
	System string `json:"system"`
}

// callAsk is one completion on the person's provider with no tools. It answers
// null when no provider is set. It needs the person's separate toggle (checked
// again here, so a revoke takes effect at once), and is limited to 10 a minute
// per artifact. The use is logged with the page's name.
func (w *pageWindow) callAsk(op string, raw []byte) ([]byte, string) {
	var a askArg
	if op != "ask" || w.svc.ask == nil || !decodeArg(raw, &a) {
		return nil, pagehost.CodeUnavailable
	}
	if a.Prompt == "" || len(a.Prompt) > maxAskPrompt || len(a.System) > maxAskSystem {
		return nil, pagehost.CodeRefused
	}
	if w.svc.askOn == nil || !w.svc.askOn(w.hash) {
		return nil, pagehost.CodeUnavailable
	}
	if !w.host.askRate(w.name).Allow(time.Now()) {
		return nil, pagehost.CodeLimit
	}
	ctx, cancel := context.WithTimeout(w.ctx, askTimeout)
	defer cancel()
	text, configured, err := w.svc.ask(ctx, a.System, a.Prompt)
	entry := ActivityEntry{At: time.Now(), Source: "page", Name: w.name, Kind: "ask", PromptBytes: len(a.Prompt) + len(a.System)}
	switch {
	case !configured:
		entry.Outcome = "no model"
	case err != nil:
		entry.Outcome = "failed"
	default:
		entry.Outcome, entry.ReplyBytes = "ok", len(text)
	}
	if w.svc.activity != nil {
		w.svc.activity(entry)
	}
	if !configured {
		return []byte("null"), ""
	}
	if err != nil {
		if w.host.logger != nil {
			w.host.logger.Debug("page ask failed", "artifact", w.name, "error", err)
		}
		return nil, pagehost.CodeModel
	}
	b, merr := json.Marshal(map[string]string{"text": text})
	if merr != nil {
		return nil, pagehost.CodeModel
	}
	return b, ""
}
