package bundle

import (
	"context"
	"fmt"
	"sort"

	"github.com/SandwichLabs/vibe/internal/skills"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

// ContentItem is one notebook, query or skill that can go in a bundle.
type ContentItem struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// ContentConnection is one connection definition that could go in a bundle.
// Portable is false (with Reason) for one that cannot leave this computer, such
// as a file path outside the workspace.
type ContentConnection struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Summary  string `json:"summary"`
	Scope    string `json:"scope"` // "team" or "this computer"
	Portable bool   `json:"portable"`
	Reason   string `json:"reason,omitempty"`
}

// Contents is everything an Export dialog can offer from a workspace.
type Contents struct {
	WorkspaceName string              `json:"workspaceName"`
	Notebooks     []ContentItem       `json:"notebooks"`
	Queries       []ContentItem       `json:"queries"`
	Skills        []ContentItem       `json:"skills"`
	Connections   []ContentConnection `json:"connections"`
}

// ContentsOf lists what t could export: its readable notebooks, queries and
// user skills (built-ins ship with Vibe), and its shared and own connection
// definitions, each marked portable or not. The workspace's own default
// database is left out: a bundle has no data to go with it.
func ContentsOf(ctx context.Context, t Target) (*Contents, error) {
	out := &Contents{WorkspaceName: t.Name(), Notebooks: []ContentItem{}, Queries: []ContentItem{}, Skills: []ContentItem{}, Connections: []ContentConnection{}}
	nbs, err := t.Notebooks().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list notebooks: %w", err)
	}
	for _, n := range nbs {
		if n.Error == "" {
			out.Notebooks = append(out.Notebooks, ContentItem{ID: n.ID, Title: n.Title})
		}
	}
	qs, err := t.Queries().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list queries: %w", err)
	}
	for _, q := range qs {
		if q.Error == "" {
			out.Queries = append(out.Queries, ContentItem{ID: q.ID, Title: q.Title})
		}
	}
	sks, err := t.Skills().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list skills: %w", err)
	}
	for _, s := range sks {
		if s.Error == "" && s.Source != skills.SourceBuiltin {
			out.Skills = append(out.Skills, ContentItem{ID: s.Name, Title: s.Description})
		}
	}
	out.Connections = append(contentConnections("team", t.Config().Connections), contentConnections("this computer", t.Local().Connections)...)
	sort.SliceStable(out.Connections, func(i, j int) bool { return out.Connections[i].Name < out.Connections[j].Name })
	return out, nil
}

func contentConnections(scope string, defs []workspace.ConnectionDef) []ContentConnection {
	out := []ContentConnection{}
	for _, d := range defs {
		if d.Name == workspace.DefaultConnectionName && d.Path == workspace.DefaultConnectionPath {
			continue // every workspace has its own; a bundle has no data to go with it
		}
		c := ContentConnection{Name: d.Name, Type: d.Type, Scope: scope, Portable: true}
		if _, _, err := SanitizeConnection(d); err != nil {
			c.Portable, c.Reason = false, PortableReason(err)
		}
		c.Summary = ConnSummary(d)
		out = append(out, c)
	}
	return out
}
