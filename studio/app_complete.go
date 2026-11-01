package main

import (
	"fmt"

	"github.com/SandwichLabs/assay/internal/lsp"
	"github.com/SandwichLabs/assay/internal/session"
)

// CompleteSQLRequest is one completion request from the Query Explorer or a
// notebook SQL cell. Offset is in UTF-16 code units (what the editor counts).
type CompleteSQLRequest struct {
	SQL        string `json:"sql"`
	Offset     int    `json:"offset"`
	Connection string `json:"connection"` // "" = the workspace default
}

// CompletionItem is one candidate. Kind is the lower-case lsp.ItemKind name.
type CompletionItem struct {
	Label  string `json:"label"`
	Kind   string `json:"kind"`
	Detail string `json:"detail,omitempty"`
	Insert string `json:"insert,omitempty"` // when it differs from Label (quoted names)
}

// CompleteSQLResult is the answer: the items replace the text from From (a
// UTF-16 offset) to the cursor.
type CompleteSQLResult struct {
	From     int              `json:"from"`
	Items    []CompletionItem `json:"items"`
	Stale    []string         `json:"stale,omitempty"`    // connections served from the saved snapshot
	Building bool             `json:"building,omitempty"` // the catalog is being rebuilt
	// Incomplete: the list was cut at the server's item cap, so the editor must
	// ask again as the word grows instead of filtering it locally.
	Incomplete bool `json:"incomplete,omitempty"`
}

// CompleteSQL completes SQL from the live catalog. It never connects a
// connection and never runs the SQL; the catalog holds names and types only.
func (a *App) CompleteSQL(req CompleteSQLRequest) (*CompleteSQLResult, error) {
	if req.Connection != "" {
		if err := session.ValidateName(req.Connection); err != nil {
			return nil, fmt.Errorf("unknown connection %q", req.Connection)
		}
	}
	l := a.liveCat.Load()
	if l == nil {
		return nil, errNoWorkspace
	}
	return toCompleteResult(l.Complete(req.SQL, req.Offset, req.Connection)), nil
}

func toCompleteResult(r lsp.CompleteResult) *CompleteSQLResult {
	out := &CompleteSQLResult{From: r.From, Items: make([]CompletionItem, 0, len(r.Items)), Stale: r.Stale, Building: r.Building, Incomplete: r.Incomplete}
	for _, it := range r.Items {
		out.Items = append(out.Items, CompletionItem{Label: it.Label, Kind: it.Kind.String(), Detail: it.Detail, Insert: it.Insert})
	}
	return out
}
