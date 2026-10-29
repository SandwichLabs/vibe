package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/filestore"
	"github.com/SandwichLabs/vibe/internal/semantics"
)

func TestResolveConflict_Semantics(t *testing.T) {
	ws, sess := conflictFixture(t)
	ctx := context.Background()
	m, err := semantics.DecodeMetric("net_revenue", []byte(semanticsMetric))
	require.NoError(t, err)
	require.NoError(t, ws.Semantics().PutMetric(ctx, m))
	path := filepath.Join(ws.Root(), "semantics", "metrics", "net_revenue.yaml")
	local := map[string]any{"text": semanticsMetric + "title: Mine\n"}
	req := func(action, rev string) ConflictRequest {
		return ConflictRequest{Kind: ConflictSemantics, ID: "metrics/net_revenue", Action: action, Rev: rev, Data: local}
	}

	// Someone else edits the file.
	appendToFile(t, path, "owner: someone else\n")
	disk, err := ws.Semantics().Metric(ctx, "net_revenue")
	require.NoError(t, err)

	res, err := ResolveConflict(ctx, ws, sess, req(ConflictReload, ""))
	require.NoError(t, err)
	assert.Equal(t, disk.Rev, res.Rev)

	// Keep mine against a stale revision is a fresh conflict and changes nothing.
	_, err = ResolveConflict(ctx, ws, sess, req(ConflictOverwrite, m.Rev))
	assert.ErrorIs(t, err, filestore.ErrConflict)
	b, _ := os.ReadFile(path)
	assert.Contains(t, string(b), "owner: someone else")

	// Save as copy leaves the disk version alone and never overwrites an earlier copy.
	cp, err := ResolveConflict(ctx, ws, sess, req(ConflictSaveCopy, ""))
	require.NoError(t, err)
	assert.Equal(t, "metrics/net_revenue_copy", cp.ID)
	cp2, err := ResolveConflict(ctx, ws, sess, req(ConflictSaveCopy, ""))
	require.NoError(t, err)
	assert.Equal(t, "metrics/net_revenue_copy_2", cp2.ID)
	got, err := ws.Semantics().Metric(ctx, "net_revenue_copy")
	require.NoError(t, err)
	assert.Equal(t, "Mine", got.Title)

	// Keep mine against the revision the banner was raised against wins, bytes as written.
	res, err = ResolveConflict(ctx, ws, sess, req(ConflictOverwrite, disk.Rev))
	require.NoError(t, err)
	b, _ = os.ReadFile(path)
	assert.Equal(t, semanticsMetric+"title: Mine\n", string(b))
	assert.NotEqual(t, disk.Rev, res.Rev)

	// Deleted on disk: written back.
	require.NoError(t, os.Remove(path))
	_, err = ResolveConflict(ctx, ws, sess, req(ConflictOverwrite, res.Rev))
	require.NoError(t, err)
	_, err = os.Stat(path)
	assert.NoError(t, err)

	// An invalid edit is refused; the glossary has no copy.
	local = map[string]any{"text": "version: 1\n"}
	_, err = ResolveConflict(ctx, ws, sess, req(ConflictOverwrite, ""))
	assert.ErrorIs(t, err, semantics.ErrInvalid)
	_, err = ResolveConflict(ctx, ws, sess, ConflictRequest{Kind: ConflictSemantics, ID: "glossary", Action: ConflictSaveCopy, Data: map[string]any{"text": "## a\n"}})
	assert.Error(t, err)
	_, err = ResolveConflict(ctx, ws, sess, ConflictRequest{Kind: ConflictSemantics, ID: "nope/x", Action: ConflictReload})
	assert.Error(t, err)
}
