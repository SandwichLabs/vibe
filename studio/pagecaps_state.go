package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/SandwichLabs/vibe/internal/jsonenc"
	"github.com/SandwichLabs/vibe/internal/pagehost"
	"github.com/SandwichLabs/vibe/internal/pagestate"
)

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }

type stateArg struct {
	Key    string          `json:"key"`
	Value  json.RawMessage `json:"value"`
	Prefix string          `json:"prefix"`
}

// stateCode maps a store error to a fixed code.
func stateCode(err error) string {
	switch {
	case errors.Is(err, pagestate.ErrBadKey), errors.Is(err, pagestate.ErrBadValue):
		return pagehost.CodeRefused
	case errors.Is(err, pagestate.ErrTooLarge):
		return pagehost.CodeTooLarge
	}
	return pagehost.CodeUnavailable
}

// callState is get, set, delete and list over the artifact's own store, which
// the window fixed when it opened.
func (w *pageWindow) callState(op string, raw []byte) ([]byte, string) {
	var a stateArg
	if !decodeArg(raw, &a) || w.store == nil {
		return nil, pagehost.CodeRefused
	}
	switch op {
	case "get":
		v, ok, err := w.store.Get(a.Key)
		if err != nil {
			return nil, stateCode(err)
		}
		if !ok {
			return []byte("null"), ""
		}
		return v, ""
	case "set":
		if len(a.Value) == 0 {
			return nil, pagehost.CodeRefused
		}
		if err := w.store.Set(a.Key, a.Value, w); err != nil {
			return nil, stateCode(err)
		}
		return []byte("true"), ""
	case "delete":
		if err := w.store.Delete(a.Key, w); err != nil {
			return nil, stateCode(err)
		}
		return []byte("true"), ""
	case "list":
		keys, err := w.store.List(a.Prefix)
		if err != nil {
			return nil, stateCode(err)
		}
		b, err := jsonenc.Encode(keys, "")
		if err != nil {
			return nil, pagehost.CodeUnavailable
		}
		return bytes.TrimRight(b, "\n"), ""
	}
	return nil, pagehost.CodeUnavailable
}

// subscribeState tells the page when another window of the same artifact wrote.
func (w *pageWindow) subscribeState() {
	if w.store == nil {
		return
	}
	w.unsub = w.store.Subscribe(w, func(c pagestate.Change) {
		ev := map[string]any{"key": c.Key, "deleted": c.Deleted}
		if !c.Deleted {
			ev["value"] = c.Value
		}
		b, err := jsonenc.Encode(ev, "")
		if err != nil {
			return
		}
		b = bytes.TrimRight(b, "\n")
		_ = w.out.Send(&pagehost.Message{T: pagehost.TypePush, Cap: "state", Op: "change", Arg: b})
	})
}
