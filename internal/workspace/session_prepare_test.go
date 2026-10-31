package workspace

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/SandwichLabs/vibe/internal/connections"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withPreparer replaces the preparer OpenSession runs for a type: the seam that
// proves OpenSession dispatches by type instead of branching on it.
func withPreparer(typ string, p preparer) SessionOption {
	return func(o *sessionOpts) { o.preparers[typ] = p }
}

// failingPreparer is an injected preparer that refuses every connection of its type.
type failingPreparer struct{ validatePreparer }

func (failingPreparer) Prepare(context.Context, prepareEnv, ResolvedConnection, connections.Def, *session.Spec) error {
	return errors.New("refused by the injected preparer")
}

func TestWithPreparerReplacesTheStepForAType(t *testing.T) {
	dir := t.TempDir()
	ws := writeVibe(t, dir, "connections:\n  - {name: mine, type: duckdb, path: data/mine.duckdb, read_only: false}\n")
	sess, issues, err := OpenSession(context.Background(), ws, nil, withPreparer("duckdb", failingPreparer{}))
	require.NoError(t, err, "a connection that cannot be set up never fails the hub")
	defer sess.Close()
	require.NotEmpty(t, issues)
	for _, i := range issues {
		assert.Contains(t, i.Message, "refused by the injected preparer")
	}
	assert.NoFileExists(t, filepath.Join(dir, "data", "mine.duckdb"), "nothing was prepared or attached")
	for _, c := range sess.Connections() {
		assert.NotEqual(t, "mine", c.Name, "a connection whose preparer failed is left out")
	}
}

func TestSkipLocalFilesRegistersWithoutAttachingOrCreating(t *testing.T) {
	dir := t.TempDir()
	ws := writeVibe(t, dir, "connections:\n  - {name: mine, type: duckdb, path: data/mine.duckdb, read_only: false}\n")
	sess, issues, err := OpenSession(context.Background(), ws, nil, WithSkipLocalFiles())
	require.NoError(t, err)
	defer sess.Close()
	assert.Empty(t, issues)
	assert.NoFileExists(t, filepath.Join(dir, "data", "mine.duckdb"))
	var seen bool
	for _, c := range sess.Connections() {
		if c.Name == "mine" {
			seen = true
			assert.NotEqual(t, session.StatusOK, c.Kind, "registered, not attached")
		}
	}
	assert.True(t, seen)
}

func TestForceReadOnlyReportsAMissingLocalFileInsteadOfCreatingIt(t *testing.T) {
	dir := t.TempDir()
	ws := writeVibe(t, dir, "connections:\n  - {name: mine, type: duckdb, path: data/mine.duckdb, read_only: false}\n")
	sess, issues, err := OpenSession(context.Background(), ws, nil, WithForceReadOnly())
	require.NoError(t, err)
	defer sess.Close()
	require.Len(t, issues, 1)
	assert.Equal(t, "mine", issues[0].Connection)
	assert.Contains(t, issues[0].Message, "no data file yet")
	assert.NoDirExists(t, filepath.Join(dir, "data"), "a read-only open creates no folder")
}

func TestMotherDuckPreparerNeverAttachesReadOnly(t *testing.T) {
	p := motherduckPreparer{}
	ro, err := p.ReadOnly(ResolvedConnection{Resolved: ConnectionDef{Type: "motherduck"}}, true)
	require.NoError(t, err)
	assert.False(t, ro, "a read-only open leaves it registered but unconnectable, never read-only")

	_, err = p.ReadOnly(ResolvedConnection{Resolved: ConnectionDef{Type: "motherduck", ReadOnly: boolPtr(true)}}, false)
	require.Error(t, err)
	assert.Equal(t, connections.ErrMotherDuckReadOnly, err.Error())
}

func TestDefaultPreparersCoverEveryEagerAndSpecialType(t *testing.T) {
	o := sessionOpts{preparers: defaultPreparers()}
	assert.True(t, o.preparerFor("duckdb").Eager())
	assert.True(t, o.preparerFor("sqlite").Eager())
	for _, typ := range []string{"postgres", "mysql", "motherduck", "ducklake", "s3", "unknown-type"} {
		assert.False(t, o.preparerFor(typ).Eager(), typ)
	}
}

// A connection type that needs preparing (it takes a hub token, or it
// is writable and its definition names a local path) must have an entry in defaultPreparers: the
// fallback only validates, so a new such type would otherwise be silently
// attached with no folder, claim or read-only rule.
func TestEveryTypeThatNeedsPreparationHasAPreparer(t *testing.T) {
	preparers := defaultPreparers()
	usesHubToken := map[string]bool{}
	for _, typ := range connections.Types() {
		if h, ok := typ.(session.HubTokenType); ok && h.UsesHubToken() {
			usesHubToken[typ.Name()] = true
		}
	}
	for _, name := range connections.AllTypeNames() {
		info, err := connections.TypeInfoFor(name)
		require.NoError(t, err)
		needs := usesHubToken[name]
		for _, f := range info.Fields {
			if f.Kind == connections.KindPath && info.Capabilities.Writable {
				needs = true
			}
		}
		if needs {
			assert.Contains(t, preparers, name, "connection type %q takes a path or the hub token: add it to defaultPreparers", name)
		}
	}
}

func boolPtr(b bool) *bool { return &b }

// attachSQLFor is the attach statement the session would run for spec.
func attachSQLFor(t *testing.T, spec session.Spec) ([]string, error) {
	t.Helper()
	for _, ct := range connections.Types() {
		if ct.Name() == spec.Type {
			return ct.AttachSQL(spec)
		}
	}
	t.Fatalf("no connection type %q", spec.Type)
	return nil, nil
}

// A read-only open leaves MotherDuck registered but unconnectable (it cannot be
// attached read-only); a policy-gated one (`vibe semantics check|profile`,
// where every statement is the agent policy's read-only path) attaches it as
// defined. Everything else stays read-only either way (#302).
func TestPolicyGatedReadOnlyReachesMotherDuckAndKeepsTheRestReadOnly(t *testing.T) {
	ws, _, _ := memWorkspace(t, nil)
	require.NoError(t, ws.AddConnection(ScopeShared, ConnectionDef{Name: "md", Type: "motherduck", Database: "d"}))
	require.NoError(t, ws.AddConnection(ScopeShared, pgConn("pg")))

	plain, err := SessionSpec(ws, "md", WithForceReadOnly())
	require.NoError(t, err)
	_, err = attachSQLFor(t, plain)
	require.Error(t, err, "a plain read-only open still refuses MotherDuck")
	assert.Contains(t, err.Error(), connections.ErrMotherDuckReadOnly)

	gated, err := SessionSpec(ws, "md", WithForceReadOnly(), WithPolicyGatedRuns())
	require.NoError(t, err)
	stmts, err := attachSQLFor(t, gated)
	require.NoError(t, err)
	assert.Equal(t, []string{`ATTACH 'md:d' AS "md"`}, stmts)

	// The preparer OpenSession runs agrees with SessionSpec.
	c, ok := ws.Connection("md")
	require.True(t, ok)
	rc, err := ResolveConnection(ws, c, nil)
	require.NoError(t, err)
	for _, tc := range []struct {
		env    prepareEnv
		forced bool
	}{
		{prepareEnv{ForceReadOnly: true}, true},
		{prepareEnv{ForceReadOnly: true, PolicyGated: true}, false},
		{prepareEnv{}, false},
	} {
		spec := rc.Def(false).Spec()
		require.NoError(t, motherduckPreparer{}.Prepare(context.Background(), tc.env, rc, rc.Def(false), &spec))
		assert.Equal(t, tc.forced, spec.Params["forced_read_only"] != "", "%+v", tc.env)
	}

	// Every connection that can be attached read-only still is.
	for _, name := range []string{"pg", "local"} {
		spec, err := SessionSpec(ws, name, WithForceReadOnly(), WithPolicyGatedRuns())
		require.NoError(t, err, name)
		assert.True(t, spec.ReadOnly, name)
	}
}

// A policy-gated read-only session still attaches a local DuckDB file read-only.
func TestPolicyGatedReadOnlySessionKeepsLocalFilesReadOnly(t *testing.T) {
	ctx := context.Background()
	ws, _, _ := memWorkspace(t, nil)
	rw, issues, err := OpenSession(ctx, ws, nil)
	require.NoError(t, err)
	require.Empty(t, issues)
	_, err = rw.Exec(ctx, "CREATE TABLE t AS SELECT 1 AS id")
	require.NoError(t, err)
	require.NoError(t, rw.Close())

	ro, issues, err := OpenSession(ctx, ws, nil, WithForceReadOnly(), WithPolicyGatedRuns())
	require.NoError(t, err)
	defer ro.Close() //nolint:errcheck
	require.Empty(t, issues)
	_, err = ro.Exec(ctx, "INSERT INTO t VALUES (2)")
	require.Error(t, err, "the local file is attached read-only")
}
