//go:build integration

package connections_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/assay/internal/connections"
	"github.com/SandwichLabs/assay/internal/secrets"
	"github.com/SandwichLabs/assay/internal/session"
)

func minioStore(ep, scope string) connections.Def {
	return connections.Def{Name: "store", Type: "s3", Scope: scope, Region: "us-east-1",
		Options: map[string]string{"endpoint": ep, "url_style": "path", "use_ssl": "false"}}
}

// A lake whose catalog is a URL is tested for real through its storage
// connection, in the same throwaway session: the catalog is vetted, the
// tables are counted. A wrong storage key is the storage connection's failure;
// a storage secret scoped narrower than the lake never reads the lake; a
// crafted catalog is still refused through Test.
func TestTestLakeThroughItsStorageOnMinIO(t *testing.T) {
	ep := startMinIO(t)
	ctx := context.Background()
	good := secrets.Credential{KeyID: "minioadmin", Secret: minioSecret}
	cat, conn := minioLake(t, ep, "sqlite")
	url := uploadCatalog(t, conn, cat, "sqlite", "catalog.db")
	lake := urlLakeDef(url, "sqlite")
	ext := connections.WithTestExtensionDir(extDir(t))

	t.Run("reads the lake for real", func(t *testing.T) {
		res := connections.Test(ctx, lake, secrets.Credential{}, connections.WithInstall(true), ext,
			connections.WithRequiredStorage(minioStore(ep, "s3://lake/"), good))
		require.True(t, res.OK, res.Error)
		assert.Equal(t, 1, res.Count, "the lake's tables are counted")
		assert.NotContains(t, res.Detail, "connect the lake from the rail")
		assert.Empty(t, res.Requirement)
	})

	t.Run("a wrong storage key is the storage connection's failure", func(t *testing.T) {
		red := secrets.NewRedactor()
		res := connections.Test(ctx, lake, secrets.Credential{}, ext, connections.WithTestRedactor(red),
			connections.WithRequiredStorage(minioStore(ep, "s3://lake/"), secrets.Credential{KeyID: "minioadmin", Secret: "WRONG_needle_k"}))
		require.False(t, res.OK)
		assert.Equal(t, "store", res.Requirement)
		assert.Equal(t, session.StatusNeedsCredentials, res.Status)
		assert.Equal(t, connections.CodeBadSignin, res.Code)
		for _, s := range []string{res.Error, res.Message, res.Detail} {
			assert.NotContains(t, s, "WRONG_needle_k")
			assert.NotContains(t, s, minioSecret)
		}
	})

	t.Run("a storage scope that does not cover the lake is refused, nothing dialled", func(t *testing.T) {
		// The catalog is outside s3://lake/sqlite/data/: refused before any
		// attach, so no request is made (not to MinIO, not to AWS).
		res := connections.Test(ctx, lake, secrets.Credential{}, ext,
			connections.WithRequiredStorage(minioStore(ep, "s3://lake/sqlite/data/"), good))
		require.False(t, res.OK)
		assert.Contains(t, res.Error, "nothing was read")
		assert.Empty(t, res.Requirement)
		assert.NotContains(t, res.Error, minioSecret)
	})

	t.Run("a crafted catalog is still refused", func(t *testing.T) {
		bad, bconn := minioLake(t, ep, "sqlite")
		tamperCatalog(t, bconn, bad, "sqlite", "CREATE TABLE c.notes (a INTEGER)")
		badURL := uploadCatalog(t, bconn, bad, "sqlite", "crafted.db")
		res := connections.Test(ctx, urlLakeDef(badURL, "sqlite"), secrets.Credential{}, ext,
			connections.WithRequiredStorage(minioStore(ep, "s3://lake/"), good))
		require.False(t, res.OK)
		assert.Contains(t, res.Error, "refusing")
		assert.Empty(t, res.Requirement)
	})

	t.Run("without a covering storage connection nothing is read", func(t *testing.T) {
		res := connections.Test(ctx, lake, secrets.Credential{}, ext)
		require.True(t, res.OK, res.Error)
		assert.Contains(t, res.Detail, "connect the lake from the rail")
	})
}
