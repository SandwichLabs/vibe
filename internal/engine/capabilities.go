package engine

import (
	"context"
	"fmt"

	"github.com/SandwichLabs/vibe/internal/connections"
	"github.com/SandwichLabs/vibe/internal/session"
)

// TableCapabilities says whether a table can be edited and, when not, why.
type TableCapabilities struct {
	Connection string `json:"connection"`
	Editable   bool   `json:"editable"`
	// Reason is connection-read-only | view | locked | workspace-read-only
	// ("" when editable).
	Reason string `json:"reason"`
	// Text is the plain-language reason for the banner.
	Text string `json:"text"`
}

// DescribeTableCapabilities explains why a table is read-only, for the
// datasheet's banner. It never dials. workspaceReadOnly is true when another
// program holds the workspace (local connections are then read-only; remote
// ones are not affected). The first reason that applies wins: locked by another
// program, workspace read-only, connection read-only, then view.
func DescribeTableCapabilities(ctx context.Context, sess session.Session, table string, workspaceReadOnly bool) (*TableCapabilities, error) {
	ref, err := ParseTable(table)
	if err != nil {
		return nil, fmt.Errorf("invalid table name")
	}
	if resolved, rerr := sess.Resolve(ctx, ref); rerr == nil {
		ref = resolved
	}
	caps := &TableCapabilities{Connection: ref.Catalog, Editable: true}
	var st *session.Status
	if s, ok := sess.Status(ref.Catalog); ok {
		st = &s
	}
	switch {
	case st != nil && st.Kind == session.StatusLockedBy:
		caps.Editable, caps.Reason = false, "locked"
		caps.Text = fmt.Sprintf("%s is in use by another program, so its tables can only be read until it lets go.", ref.Catalog)
	case workspaceReadOnly && st != nil && !connections.IsRemote(st.Type):
		caps.Editable, caps.Reason = false, "workspace-read-only"
		caps.Text = "This workspace is open read-only because another program has it."
	case st != nil && st.ReadOnly:
		caps.Editable, caps.Reason = false, "connection-read-only"
		caps.Text = fmt.Sprintf("%s is set to read-only, so rows can't be added, edited or deleted here.", ref.Catalog)
	}
	if caps.Editable {
		if tabs, err := sess.ListTables(ctx, ref.Catalog, ref.Schema); err == nil {
			for _, t := range tabs {
				if t.Name == ref.Name && RelationKind(t.Type) == "view" {
					caps.Editable, caps.Reason, caps.Text = false, "view", "This is a view, so it can't be edited."
				}
			}
		}
	}
	return caps, nil
}
