package main

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/slaw/internal/session"
	"github.com/SandwichLabs/slaw/internal/session/sessiontest"
	"github.com/SandwichLabs/slaw/internal/workspace"
)

// A ready catalog-less (storage) connection has no tables to list: the Tables
// view's groups show it as ok, not as the "is storage, not a database" error
// that hid every other connection behind a "can't list" page.
func TestListCatalogGroupsSkipsACatalogLessConnection(t *testing.T) {
	app := newOpenApp(t)
	require.NoError(t, app.ws.AddConnection(workspace.ScopeLocal, workspace.ConnectionDef{Name: "store", Type: "s3", Scope: "s3://bucket/", Region: "us-east-1"}))
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	fake := sessiontest.ForTests(db, session.WithConnectionTypes(sessiontest.ScopeAcceptingType{TypeName: "s3"}))
	require.NoError(t, fake.Attach(context.Background(), session.Spec{Name: "store", Type: "s3"}))
	app.mu.Lock()
	real := app.sess
	app.sess = fake
	app.mu.Unlock()
	t.Cleanup(func() { app.mu.Lock(); app.sess = real; app.mu.Unlock() })

	groups, err := app.ListCatalogGroups()
	require.NoError(t, err)
	var found bool
	for _, g := range groups {
		if g.Connection == "store" {
			found = true
			assert.Equal(t, "ok", g.Status, g.Detail)
			assert.Empty(t, g.Tables)
		}
	}
	assert.True(t, found)
}
