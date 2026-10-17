package bundle_test

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/agents"
	"github.com/SandwichLabs/vibe/internal/artifacts"
	"github.com/SandwichLabs/vibe/internal/bundle"
	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

const extManifest = `version: 1
title: Sales by region
parameters:
  - name: min_id
    type: integer
    default: "0"
queries:
  - name: rows
    sql: SELECT 1 AS id WHERE 1 >= $min_id
`

const extView = `{"title":"Sales","components":[{"id":"t","type":"table","query":"rows"}]}`

func extWS(t *testing.T) workspace.Workspace {
	t.Helper()
	res, err := workspace.Init(filepath.Join(t.TempDir(), "acme"))
	require.NoError(t, err)
	_ = res.Workspace.Close()
	store := secrets.NewMemoryStore()
	r := secrets.NewResolver(secrets.WithKeychain(store), secrets.WithEnv(func(string) string { return "" }))
	ws, err := workspace.Open(res.Workspace.Root(), workspace.WithConfigDir(t.TempDir()),
		workspace.WithUserConfig(workspace.UserConfig{Version: workspace.ConfigVersion}), workspace.WithResolver(r))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ws.Close() })
	return ws
}

func extPut(t *testing.T, ws workspace.Workspace, name, manifest string) {
	t.Helper()
	require.NoError(t, ws.Artifacts().Put(context.Background(), &artifacts.Artifact{Name: name, ManifestText: []byte(manifest), View: []byte(extView)}, artifacts.WithRev("")))
}

// A hostile query is structurally valid, so it imports; it is refused when it
// runs, by the same gate every artifact query goes through.
func TestArtifact_ImportedHostileQueryIsRefusedAtRun(t *testing.T) {
	hostile := strings.Replace(extManifest, "SELECT 1 AS id WHERE 1 >= $min_id", "SELECT * FROM read_csv('/etc/passwd') WHERE 1 >= $min_id", 1)
	src := extWS(t)
	extPut(t, src, "sales", hostile)
	pkg, err := bundle.Open(exportOne(t, src, "sales"), "")
	require.NoError(t, err, "the bundle layer is structural only")

	dst := extWS(t)
	res, err := pkg.Import(context.Background(), dst, bundle.ImportOptions{})
	require.NoError(t, err)
	require.Equal(t, "created", res.Outcomes[0].Result, res.Outcomes[0].Error)

	sess, err := session.New(session.WithTempDirectory(t.TempDir()), session.WithExtensionDirectory(t.TempDir()), session.WithSecretDirectory(t.TempDir()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sess.Close() })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := agents.NewHandler(workspace.NewAgentAccess(dst, sess), engine.NewManager(sess, engine.WithLogger(logger)),
		&agents.SandboxConfig{Mode: agents.ModeFull, MaxQueryRows: 100}, logger)

	_, _, err = h.RunArtifactQuery(context.Background(), "sales", "rows", nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, agents.ErrReadOnlyBindingDenied)

	// And the same artifact with a harmless query runs, so the refusal is the policy's.
	extPut(t, dst, "ok", extManifest)
	rs, _, err := h.RunArtifactQuery(context.Background(), "ok", "rows", nil)
	require.NoError(t, err)
	assert.Len(t, rs.Rows, 1)
}

func exportOne(t *testing.T, ws workspace.Workspace, name string) []byte {
	t.Helper()
	res, err := bundle.Export(context.Background(), ws, bundle.ExportOptions{Selection: bundle.Selection{Artifacts: []string{name}}})
	require.NoError(t, err)
	return res.Data
}
