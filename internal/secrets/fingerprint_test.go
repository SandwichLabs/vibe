package secrets_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

// A credential is bound to workspace.Fingerprint: type, host, port and
// database or bucket scope. Changing any of them (a pulled commit) means the
// saved secret is not sent to the new server.
func TestCredentialBoundToWorkspaceFingerprint(t *testing.T) {
	r := secrets.NewResolver(secrets.WithKeychain(&secrets.Mock{}))
	ctx := context.Background()
	ref := secrets.WorkspaceRef("ws-1", "prod_pg")
	def := workspace.ConnectionDef{Name: "prod_pg", Type: "postgres", Host: "db.internal", Port: 5432, Database: "app"}
	bind := func(d workspace.ConnectionDef) secrets.Binding {
		return secrets.Binding{Fingerprint: workspace.Fingerprint(d), Server: d.Host}
	}
	require.NoError(t, r.SetConnectionCredentials(ctx, ref, "keychain", secrets.Credential{User: "rosa", Password: "pw-NEEDLE-1"}, bind(def)))

	_, err := r.Resolve(ctx, ref, "keychain", bind(def))
	require.NoError(t, err)

	for name, mutate := range map[string]func(*workspace.ConnectionDef){
		"host":     func(d *workspace.ConnectionDef) { d.Host = "db-new.internal" },
		"port":     func(d *workspace.ConnectionDef) { d.Port = 6432 },
		"database": func(d *workspace.ConnectionDef) { d.Database = "other" },
		"type":     func(d *workspace.ConnectionDef) { d.Type = "mysql" },
		"endpoint": func(d *workspace.ConnectionDef) { d.Options = map[string]string{"endpoint": "evil.example"} },
		"hostaddr": func(d *workspace.ConnectionDef) { d.Options = map[string]string{"hostaddr": "10.0.0.9"} },
		"region":   func(d *workspace.ConnectionDef) { d.Region = "eu-west-1" },
		"path":     func(d *workspace.ConnectionDef) { d.Path = "/other.db" },
		"scope":    func(d *workspace.ConnectionDef) { d.Scope = "s3://elsewhere/" },
	} {
		moved := def
		mutate(&moved)
		_, err := r.Resolve(ctx, ref, "keychain", bind(moved))
		var fc *secrets.ErrFingerprintChanged
		require.ErrorAs(t, err, &fc, name)
		assert.NotContains(t, err.Error(), "NEEDLE", name)
		st := r.GetConnectionCredentialStatus(ctx, ref, "keychain", bind(moved))
		assert.False(t, st.FingerprintMatches, name)
	}
	// Non-identity fields do not break the binding.
	same := def
	same.Name = "renamed"
	same.ReadOnly = workspace.Bool(false)
	_, err = r.Resolve(ctx, ref, "keychain", bind(same))
	assert.NoError(t, err)
}
