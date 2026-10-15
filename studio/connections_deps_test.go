package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/workspace"
)

// A lake follows the storage connection it was resolved against: when that
// connection goes away the lake is registered again, so its scope check runs
// against what is left (an issue, not a stale spec that passed the old check).
func TestSyncReregistersDependentsOfAChangedStorageConnection(t *testing.T) {
	app, _ := connApp(t)
	ro := true
	f := &ro
	require.NoError(t, app.ws.AddConnection(workspace.ScopeLocal, workspace.ConnectionDef{Name: "store", Type: "s3", Scope: "s3://bucket/", Region: "us-east-1"}))
	require.NoError(t, app.ws.AddConnection(workspace.ScopeLocal, workspace.ConnectionDef{Name: "lake", Type: "ducklake", Path: "s3://bucket/lake/catalog.db", ReadOnly: f,
		Options: map[string]string{"catalog": "sqlite", "data_path": "s3://bucket/lake/data/"}}))
	app.syncConnections()
	app.connSigMu.Lock()
	assert.Equal(t, []string{"store"}, app.connDeps["lake"].Requires)
	app.connSigMu.Unlock()
	assert.Empty(t, app.issues)

	// Only the storage connection changes; the lake's own definition does not.
	require.NoError(t, app.ws.RemoveConnection(context.Background(), workspace.ScopeLocal, "store"))
	app.syncConnections()
	require.Len(t, app.issues, 1)
	assert.Equal(t, "lake", app.issues[0].Connection)
	assert.Contains(t, app.issues[0].Message, "not inside the scope")

	// Adding a storage connection again repairs the unresolved lake.
	require.NoError(t, app.ws.AddConnection(workspace.ScopeLocal, workspace.ConnectionDef{Name: "store2", Type: "s3", Scope: "s3://bucket/", Region: "us-east-1"}))
	app.syncConnections()
	assert.Empty(t, app.issues)
	app.connSigMu.Lock()
	assert.Equal(t, []string{"store2"}, app.connDeps["lake"].Requires)
	app.connSigMu.Unlock()
}

func TestDependentsOfIgnoresUnrelatedChanges(t *testing.T) {
	cur := map[string]workspace.Connection{"a": {}, "b": {}, "c": {}}
	old := map[string]connDeps{"a": {Requires: []string{"s1"}}, "b": {Requires: []string{"s2"}}, "c": {Unresolved: true}}
	got := dependentsOf(cur, map[string]string{"s1": "s3"}, old, old)
	assert.Equal(t, map[string]bool{"a": true, "c": true}, got)
	got = dependentsOf(cur, map[string]string{"s9": "postgres"}, old, old)
	assert.Empty(t, got)
}

// A table source and the storage connection that reads it, added together: the
// table must not be preflighted before its storage connection is registered
// (map order in a sync, name order at open).
func TestSyncRegistersBeforeItPreflights(t *testing.T) {
	app, _ := connApp(t)
	require.NoError(t, app.ws.AddConnection(workspace.ScopeLocal, workspace.ConnectionDef{Name: "events", Type: "delta", Path: "s3://bucket/t/"}))
	require.NoError(t, app.ws.AddConnection(workspace.ScopeLocal, workspace.ConnectionDef{Name: "warehouse", Type: "s3", Scope: "s3://bucket/", Region: "us-east-1"}))
	app.syncConnections()
	_, sess, _, err := app.snapshot()
	require.NoError(t, err)
	st, ok := sess.Status("events")
	require.True(t, ok)
	assert.NotContains(t, st.Message, "not defined")
	assert.Empty(t, app.issues)
}
