//go:build integration

package connections_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/connections"
	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

// Iceberg needs avro and iceberg (DuckDB 1.5). With an empty extension
// directory the status must name the extension to download next, and
// following those offers one by one must end with a ready connection.
func TestIcebergConsentRoundTripNeedsAvroThenIceberg(t *testing.T) {
	ctx := context.Background()
	ws, _ := memWS(t)
	require.NoError(t, ws.AddConnection(workspace.ScopeLocal, workspace.ConnectionDef{Name: "ice", Type: "iceberg", Path: "data/ice"}))
	sess, issues, err := workspace.OpenSession(ctx, ws, nil, workspace.WithSessionOptions(session.WithExtensionDirectory(t.TempDir())))
	require.NoError(t, err)
	require.Empty(t, issues)
	defer sess.Close() //nolint:errcheck

	// Test names the same first download.
	res := connections.Test(ctx, connections.Def{Name: "ice", Type: "iceberg", Path: t.TempDir()}, secrets.Credential{}, connections.WithTestExtensionDir(t.TempDir()))
	assert.Equal(t, "avro", res.InstallExtension)

	var offered []string
	for i := 0; i < 4; i++ {
		sess.Preflight(ctx, "ice")
		st := workspace.ConnectionStates(ws, sess, nil)[0]
		if st.Status != session.StatusExtensionMissing {
			break
		}
		offered = append(offered, st.Extension)
		require.NoError(t, sess.InstallExtension(ctx, st.Extension), st.Extension)
	}
	assert.Equal(t, []string{"avro", "iceberg"}, offered)
	sess.Preflight(ctx, "ice")
	assert.Equal(t, session.StatusOK, connStatus(sess, "ice").Kind)
}
