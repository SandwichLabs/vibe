package engine

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/session"
)

func capsHub(t *testing.T, readOnly bool) session.Session {
	t.Helper()
	dir := t.TempDir()
	s, err := session.New(
		session.WithTempDirectory(filepath.Join(dir, "tmp")),
		session.WithExtensionDirectory(filepath.Join(dir, "ext")),
		session.WithSecretDirectory(filepath.Join(dir, "sec")),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	path := filepath.Join(dir, "c.duckdb")
	f, err := sql.Open("duckdb", path)
	require.NoError(t, err)
	_, err = f.Exec(`CREATE TABLE t (id INTEGER); CREATE VIEW v AS SELECT * FROM t`)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.NoError(t, s.Attach(context.Background(), session.Spec{Name: "c", Type: "duckdb", Path: path, ReadOnly: readOnly}))
	return s
}

func TestDescribeTableCapabilities_Reasons(t *testing.T) {
	ctx := context.Background()

	s := capsHub(t, false)
	c, err := DescribeTableCapabilities(ctx, s, "c.main.t", false)
	require.NoError(t, err)
	assert.Equal(t, &TableCapabilities{Connection: "c", Editable: true}, c)

	c, err = DescribeTableCapabilities(ctx, s, "c.main.v", false)
	require.NoError(t, err)
	assert.Equal(t, &TableCapabilities{Connection: "c", Reason: "view", Text: "This is a view, so it can't be edited."}, c)

	// The workspace held by another program: a local connection is read-only.
	c, err = DescribeTableCapabilities(ctx, s, "c.main.t", true)
	require.NoError(t, err)
	assert.False(t, c.Editable)
	assert.Equal(t, "workspace-read-only", c.Reason)
	assert.Equal(t, "This workspace is open read-only because another program has it.", c.Text)

	ro := capsHub(t, true)
	c, err = DescribeTableCapabilities(ctx, ro, "c.main.t", false)
	require.NoError(t, err)
	assert.False(t, c.Editable)
	assert.Equal(t, "connection-read-only", c.Reason)
	assert.Equal(t, "c is set to read-only, so rows can't be added, edited or deleted here.", c.Text)

	// Workspace read-only outranks connection read-only; both outrank "view".
	c, err = DescribeTableCapabilities(ctx, ro, "c.main.v", true)
	require.NoError(t, err)
	assert.Equal(t, "workspace-read-only", c.Reason)
	c, err = DescribeTableCapabilities(ctx, ro, "c.main.v", false)
	require.NoError(t, err)
	assert.Equal(t, "connection-read-only", c.Reason)
}

func TestDescribeTableCapabilities_InvalidName(t *testing.T) {
	s := capsHub(t, false)
	_, err := DescribeTableCapabilities(context.Background(), s, `x"; DROP`, false)
	assert.EqualError(t, err, "invalid table name")
}
