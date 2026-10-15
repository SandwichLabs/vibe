package main

// Connections that read with another connection's secret (a DuckLake or table in
// object storage, session.Spec.Requires) follow it: when the storage connection
// is edited, added or removed, the dependent is registered again, so its scope
// check runs against the new definition.

import (
	"github.com/SandwichLabs/vibe/internal/workspace"
)

// connDeps is what one connection was registered as requiring. Unresolved is
// set when its spec could not be built (for example its URL was outside every
// storage scope): any storage connection that changes may fix it.
type connDeps struct {
	Requires   []string
	Unresolved bool
}

// currentDeps reads the requirements of every connection of ws.
func currentDeps(ws workspace.Workspace) map[string]connDeps {
	out := map[string]connDeps{}
	for _, c := range ws.Connections() {
		if !usesStorageSecret(c.Type) {
			continue
		}
		spec, err := workspace.SessionSpec(ws, c.Name)
		out[c.Name] = connDeps{Requires: spec.Requires, Unresolved: err != nil}
	}
	return out
}

func usesStorageSecret(t string) bool {
	return t == "ducklake" || t == "iceberg" || t == "delta"
}

// changedNames lists the connections that are new, edited or gone.
func changedNames(cur map[string]workspace.Connection, old, next map[string]string) map[string]string {
	changed := map[string]string{} // name -> type ("" when gone)
	for n := range old {
		if _, ok := cur[n]; !ok {
			changed[n] = "gone"
		}
	}
	for n, c := range cur {
		if old[n] != next[n] {
			changed[n] = c.Type
		}
	}
	return changed
}

// dependentsOf returns the unchanged connections that required (before or now)
// a changed connection, or could not resolve while a storage connection changed.
func dependentsOf(cur map[string]workspace.Connection, changed map[string]string, oldDeps, nextDeps map[string]connDeps) map[string]bool {
	out := map[string]bool{}
	storageChanged := false
	for _, t := range changed {
		storageChanged = storageChanged || workspace.IsStorageType(t)
	}
	for n := range cur {
		if _, ok := changed[n]; ok {
			continue
		}
		for _, d := range []connDeps{oldDeps[n], nextDeps[n]} {
			if d.Unresolved && storageChanged {
				out[n] = true
			}
			for _, r := range d.Requires {
				if _, ok := changed[r]; ok {
					out[n] = true
				}
			}
		}
	}
	return out
}
