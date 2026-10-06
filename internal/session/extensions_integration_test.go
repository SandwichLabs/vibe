//go:build integration

package session

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Spike #107: extension install/load vs autoinstall/autoload, community gate,
// and enable_external_access=false (needs network for INSTALL).
func TestSpikeExtensionsAndHardening(t *testing.T) {
	extDir := filepath.Join(os.TempDir(), "vibe-spike-ext")
	require.NoError(t, os.MkdirAll(extDir, 0o755))
	dsn := "?extension_directory=" + url.QueryEscape(extDir) +
		"&autoinstall_known_extensions=false&autoload_known_extensions=false&allow_community_extensions=false&allow_persistent_secrets=false"

	t.Run("autoload off: httpfs function fails until LOAD", func(t *testing.T) {
		db := openDSN(t, dsn)
		db.SetMaxOpenConns(1)
		_, err := rows(db, "SELECT * FROM read_csv('https://example.invalid/x.csv')")
		t.Logf("https read without httpfs loaded: %v", err)
		_, err = db.Exec("INSTALL httpfs")
		require.NoError(t, err)
		_, err = db.Exec("LOAD httpfs")
		require.NoError(t, err)
		_, err = rows(db, "SELECT * FROM read_csv('https://example.invalid/x.csv')")
		t.Logf("https read with httpfs loaded (host does not resolve): %v", firstLine(err))
		require.Error(t, err)
	})

	t.Run("community extensions gate", func(t *testing.T) {
		db := openDSN(t, dsn)
		_, err := db.Exec("INSTALL h3 FROM community")
		t.Logf("INSTALL h3 FROM community with allow_community_extensions=false: %v", err)
		// FINDING: the gate is on LOAD, not INSTALL.
		_, err = db.Exec("LOAD h3")
		t.Logf("LOAD h3 with allow_community_extensions=false: %v", err)
		require.Error(t, err)
		// Can it be re-enabled at runtime?
		_, err = db.Exec("SET allow_community_extensions=true")
		t.Logf("SET allow_community_extensions=true after open: %v", err)
	})

	t.Run("lock_configuration does not stop INSTALL/LOAD", func(t *testing.T) {
		db := openDSN(t, dsn+"&lock_configuration=true")
		db.SetMaxOpenConns(1)
		_, err := db.Exec("INSTALL httpfs")
		assert.NoError(t, err)
		_, err = db.Exec("LOAD httpfs")
		assert.NoError(t, err, "LOAD not blocked by lock_configuration: the policy must deny INSTALL/LOAD")
	})

	t.Run("enable_external_access=false x INSTALL/LOAD/httpfs", func(t *testing.T) {
		db := openDSN(t, dsn)
		db.SetMaxOpenConns(1)
		for _, s := range []string{"INSTALL httpfs", "INSTALL postgres", "LOAD httpfs", "LOAD postgres"} {
			_, err := db.Exec(s)
			require.NoError(t, err, s)
		}
		allowed := t.TempDir()
		_, err := db.Exec("SET allowed_directories=['" + allowed + "']")
		require.NoError(t, err)
		_, err = db.Exec("SET enable_external_access=false")
		require.NoError(t, err)

		_, err = rows(db, "SELECT * FROM read_csv('https://example.invalid/x.csv')")
		t.Logf("preloaded httpfs, external access off, https read: %v", firstLine(err))
		require.Error(t, err)

		_, err = db.Exec("INSTALL spatial")
		t.Logf("INSTALL (new ext) with external access off: %v", err)
		require.Error(t, err)
		_, err = db.Exec("LOAD postgres")
		t.Logf("LOAD already-loaded ext with external access off: %v", err)
		_, err = db.Exec("LOAD httpfs")
		t.Logf("LOAD httpfs again: %v", err)
	})

	t.Run("fresh instance: external access off before LOAD", func(t *testing.T) {
		db := openDSN(t, dsn+"&enable_external_access=false")
		db.SetMaxOpenConns(1)
		_, err := db.Exec("LOAD httpfs")
		t.Logf("LOAD httpfs when enable_external_access=false from open: %v", err)
		_, err = db.Exec("LOAD postgres")
		t.Logf("LOAD postgres when enable_external_access=false from open: %v", err)
	})
}

// Can a hub with the postgres extension preloaded still ATTACH a remote catalog
// after enable_external_access=false? (Decides the "agent-only second hub" idea, D13.)
func TestSpikeExternalAccessOffStillAttachesRemote(t *testing.T) {
	pg := startPG(t)
	pg.psql(t, "CREATE TABLE people(id int); INSERT INTO people VALUES (1)")
	db := openWithPostgres(t, "")
	require.NoError(t, pg.createSecret(db, "pgs", pgPassword))
	_, err := db.Exec("SET allowed_directories=['" + t.TempDir() + "']")
	require.NoError(t, err)
	_, err = db.Exec("SET enable_external_access=false")
	require.NoError(t, err)

	_, err = db.Exec("ATTACH '' AS pg (TYPE postgres, SECRET pgs, READ_ONLY)")
	t.Logf("ATTACH postgres after external access off: %v", err)
	if err == nil {
		v, qerr := scalar(t.Context(), db, "SELECT count(*) FROM pg.public.people")
		t.Logf("query after attach: %s err=%v", v, qerr)
	}
	// Cross-check the other order: attach first, then lock the hub down.
	db2 := openWithPostgres(t, "")
	require.NoError(t, pg.createSecret(db2, "pgs", pgPassword))
	_, err = db2.Exec("ATTACH '' AS pg (TYPE postgres, SECRET pgs, READ_ONLY)")
	require.NoError(t, err)
	_, err = db2.Exec("SET enable_external_access=false")
	require.NoError(t, err)
	v, err := scalar(t.Context(), db2, "SELECT count(*) FROM pg.public.people")
	t.Logf("attached-before-lockdown query: %s err=%v", v, err)
	_, err = db2.Exec("ATTACH '' AS pg2 (TYPE postgres, SECRET pgs, READ_ONLY)")
	t.Logf("second ATTACH after lockdown: %v", err)
	_, err = db2.Exec("CREATE TEMPORARY SECRET late (TYPE postgres, HOST 'h', DATABASE 'd', USER 'u', PASSWORD 'p')")
	t.Logf("CREATE SECRET after lockdown: %v", err)
}
