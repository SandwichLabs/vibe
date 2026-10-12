package workspace

import (
	"sort"

	"github.com/SandwichLabs/vibe/internal/connections"
)

// ExtensionNeed is one DuckDB extension some connection type needs, and which
// of a workspace's connections need it.
type ExtensionNeed struct {
	Name string
	// NeededBy holds the names of the workspace's connections whose type needs
	// the extension, sorted; never nil.
	NeededBy []string
}

// ExtensionNeeds lists every extension any connection type needs (each once,
// sorted by name) with the connections of ws that need it. A nil ws means no
// connections: every NeededBy is empty.
func ExtensionNeeds(ws Workspace) []ExtensionNeed {
	needed := map[string][]string{}
	if ws != nil {
		for _, c := range ws.Connections() {
			if ti, err := connections.TypeInfoFor(c.Type); err == nil {
				for _, e := range ti.Extensions {
					needed[e] = append(needed[e], c.Name)
				}
			}
		}
	}
	seen := map[string]bool{}
	names := []string{}
	for _, ti := range connections.ListConnectionTypes() {
		for _, e := range ti.Extensions {
			if !seen[e] {
				seen[e] = true
				names = append(names, e)
			}
		}
	}
	sort.Strings(names)
	out := make([]ExtensionNeed, 0, len(names))
	for _, n := range names {
		nb := needed[n]
		if nb == nil {
			nb = []string{}
		}
		sort.Strings(nb)
		out = append(out, ExtensionNeed{Name: n, NeededBy: nb})
	}
	return out
}
