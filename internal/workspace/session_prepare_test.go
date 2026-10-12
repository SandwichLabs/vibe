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
