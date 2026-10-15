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

// Studio's consent loop (ConnectionStates -> InstallExtension(st.Extension)) ends
// for a lake whose needs depend on its definition: a SQLite catalog needs
// sqlite, a catalog in object storage needs httpfs, after ducklake (#118).
func TestDuckLakeConsentRoundTripFollowsTheSpec(t *testing.T) {
	cases := map[string]struct {
		def  workspace.ConnectionDef
		want []string
	}{
		"sqlite catalog": {
			def:  workspace.ConnectionDef{Name: "lake", Type: "ducklake", Path: "data/lake.db", Options: map[string]string{"catalog": "sqlite", "data_path": "data/files"}},
			want: []string{"ducklake", "sqlite"},
		},
		"url catalog": {
			def: workspace.ConnectionDef{Name: "lake", Type: "ducklake", Path: "s3://bucket/lake/c.db", ReadOnly: workspaceBool(true),
				Options: map[string]string{"catalog": "sqlite", "data_path": "s3://bucket/lake/data/"}},
			want: []string{"ducklake", "sqlite", "httpfs"},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			ws, _ := memWS(t)
			require.NoError(t, ws.AddConnection(workspace.ScopeLocal, workspace.ConnectionDef{Name: "store", Type: "s3", Scope: "s3://bucket/", Region: "us-east-1"}))
			require.NoError(t, ws.AddConnection(workspace.ScopeLocal, c.def))
			sess, issues, err := workspace.OpenSession(ctx, ws, nil, workspace.WithSessionOptions(session.WithExtensionDirectory(t.TempDir())))
			require.NoError(t, err)
			require.Empty(t, issues)
			defer sess.Close() //nolint:errcheck

			var offered []string
			for i := 0; i < 6; i++ {
				sess.Preflight(ctx, "lake")
				var st workspace.ConnectionState
				for _, s := range workspace.ConnectionStates(ws, sess, nil) {
					if s.Name == "lake" {
						st = s
					}
				}
				if st.Status != session.StatusExtensionMissing {
					break
				}
				offered = append(offered, st.Extension)
				require.NoError(t, sess.InstallExtension(ctx, st.Extension), st.Extension)
			}
			assert.Equal(t, c.want, offered)
			sess.Preflight(ctx, "lake")
			assert.NotEqual(t, session.StatusExtensionMissing, connStatus(sess, "lake").Kind)
		})
	}
}

// Test's listing counts objects in sub-folders: a scope whose objects all live
// below the first folder level is not "0 objects".
func TestStorageProbeCountsObjectsInSubfolders(t *testing.T) {
	ep := startMinIO(t)
	cat, conn := minioLake(t, ep, "sqlite")
	uploadCatalog(t, conn, cat, "sqlite", "catalog.db")
	d := connections.Def{Name: "lake", Type: "s3", Scope: "s3://lake/", Region: "us-east-1",
		Options: map[string]string{"endpoint": ep, "url_style": "path", "use_ssl": "false"}}
	res := connections.Test(context.Background(), d, secrets.Credential{KeyID: "minioadmin", Secret: minioSecret},
		connections.WithInstall(true), connections.WithTestExtensionDir(extDir(t)))
	require.True(t, res.OK, res.Error)
	assert.Greater(t, res.Count, 0, "objects under s3://lake/sqlite/ are counted")
}
