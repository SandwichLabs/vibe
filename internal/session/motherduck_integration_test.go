//go:build integration

package session

import (
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mdDSN(extra string) string {
	extDir := filepath.Join(os.TempDir(), "vibe-spike-ext")
	_ = os.MkdirAll(extDir, 0o755)
	d := "?allow_persistent_secrets=false&extension_directory=" + url.QueryEscape(extDir)
	if extra != "" {
		d += "&" + extra
	}
	return d
}

const fakeMDToken = "eyFAKE.md_token_needle_9981.sig"

// Spike #107 MotherDuck, no real token needed: where can a token be supplied,
// and does a bad token leak into error text?
func TestSpikeMotherDuckFakeToken(t *testing.T) {
	t.Run("CREATE SECRET TYPE motherduck is NOT supported on 1.4.1", func(t *testing.T) {
		db := openDSN(t, mdDSN(""))
		db.SetMaxOpenConns(1)
		_, err := db.Exec("INSTALL motherduck")
		require.NoError(t, err, "INSTALL motherduck (needs network)")
		_, err = db.Exec("LOAD motherduck")
		t.Logf("LOAD motherduck on go-duckdb v2.4.3 / DuckDB 1.4.1: %v", err)
		require.NoError(t, err)
		_, err = db.Exec("CREATE TEMPORARY SECRET md1 (TYPE motherduck, TOKEN '" + fakeMDToken + "')")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Secret type 'motherduck' not found")
		assert.NotContains(t, err.Error(), fakeMDToken)
	})

	// FINDING: with no token anywhere, ATTACH 'md:' starts an interactive SSO flow
	// (tries to open a browser, prints a login URL to stdout, blocks ~60s).
	// Gated because it is slow and noisy; run with SPIKE_MD_SSO=1 to reproduce.
	t.Run("no token anywhere: ATTACH blocks on interactive SSO", func(t *testing.T) {
		if os.Getenv("SPIKE_MD_SSO") == "" {
			t.Skip("set SPIKE_MD_SSO=1 to reproduce the ~60s browser-SSO hang")
		}
		db := openDSN(t, mdDSN(""))
		db.SetMaxOpenConns(1)
		_, _ = db.Exec("INSTALL motherduck")
		_, _ = db.Exec("LOAD motherduck")
		_, err := db.Exec("ATTACH 'md:nosuchdb?attach_mode=single' AS md2")
		t.Logf("ATTACH md without token: %v", err)
	})

	t.Run("token via SET motherduck_token after LOAD", func(t *testing.T) {
		db := openDSN(t, mdDSN(""))
		db.SetMaxOpenConns(1)
		_, _ = db.Exec("INSTALL motherduck")
		_, err := db.Exec("LOAD motherduck")
		require.NoError(t, err)
		_, err = db.Exec("SET motherduck_token='" + fakeMDToken + "'")
		t.Logf("SET motherduck_token after LOAD: %v", err)
		if err != nil {
			return
		}
		_, err = db.Exec("ATTACH 'md:nosuchdb?attach_mode=single' AS md5")
		t.Logf("ATTACH after SET token: %v", err)
		t.Logf("fake token echoed in error: %v", err != nil && strings.Contains(err.Error(), fakeMDToken))
		var v string
		_ = db.QueryRow("SELECT CAST(current_setting('motherduck_token') AS VARCHAR)").Scan(&v)
		t.Logf("current_setting('motherduck_token') readable (leak surface for agent SQL): %q", v)
		r, err := rows(db, "SELECT name, value FROM duckdb_settings() WHERE name = 'motherduck_token'")
		t.Logf("duckdb_settings() row for token: %v err=%v", r, err)
	})

	t.Run("token inside the md: connection string", func(t *testing.T) {
		if os.Getenv("SPIKE_MD_SSO") == "" {
			t.Skip("token-in-connection-string is ignored and falls into the ~60s SSO hang; SPIKE_MD_SSO=1 to reproduce")
		}
		db := openDSN(t, mdDSN(""))
		db.SetMaxOpenConns(1)
		_, _ = db.Exec("INSTALL motherduck")
		_, _ = db.Exec("LOAD motherduck")
		_, err := db.Exec("ATTACH 'md:nosuchdb?attach_mode=single&motherduck_token=" + fakeMDToken + "' AS md6")
		t.Logf("ATTACH with token in connection string: %v", err)
		r, _ := rows(db, "SELECT database_name, path FROM duckdb_databases() WHERE database_name='md6'")
		t.Logf("path shows token: %v", r)
	})

	t.Run("token as a DSN option is rejected at open", func(t *testing.T) {
		c, err := tryOpen(mdDSN("motherduck_token=" + fakeMDToken))
		if err != nil {
			// FINDING: not a recognised option at open; it only exists after LOAD motherduck.
			t.Logf("DSN motherduck_token rejected at open: %v", err)
			require.Contains(t, err.Error(), "not recognized")
			return
		}
		defer c.Close()
		c.SetMaxOpenConns(1)
		_, _ = c.Exec("INSTALL motherduck")
		_, err = c.Exec("LOAD motherduck")
		t.Logf("LOAD with motherduck_token in DSN: %v", err)
		_, err = c.Exec("ATTACH 'md:nosuchdb?attach_mode=single' AS md3")
		t.Logf("ATTACH after token via DSN: %v", err)
		t.Logf("fake token echoed in error: %v", err != nil && strings.Contains(err.Error(), fakeMDToken))
		t.Logf("current_setting(motherduck_token)=%q", func() string {
			var s string
			_ = c.QueryRow("SELECT CAST(current_setting('motherduck_token') AS VARCHAR)").Scan(&s)
			return s
		}())
		r, err := rows(c, "SELECT name FROM duckdb_settings() WHERE name LIKE '%motherduck%'")
		t.Logf("motherduck settings: %v err=%v", r, err)
	})

	t.Run("token via motherduck_token env var", func(t *testing.T) {
		t.Setenv("motherduck_token", fakeMDToken)
		db := openDSN(t, mdDSN(""))
		db.SetMaxOpenConns(1)
		_, _ = db.Exec("INSTALL motherduck")
		_, err := db.Exec("LOAD motherduck")
		t.Logf("LOAD with env token: %v", err)
		_, err = db.Exec("ATTACH 'md:nosuchdb?attach_mode=single' AS md4")
		t.Logf("ATTACH with env token: %v", err)
	})
}

// With a real token (MOTHERDUCK_TOKEN): the checklist's real questions.
// Skips cleanly otherwise; a token holder runs:
//
//	MOTHERDUCK_TOKEN=... go test -tags integration ./internal/session -run MotherDuckReal -v
func TestSpikeMotherDuckRealToken(t *testing.T) {
	tok := os.Getenv("MOTHERDUCK_TOKEN")
	if tok == "" {
		t.Skip("MOTHERDUCK_TOKEN not set")
	}
	open := func(t *testing.T, extra string) (*sql.DB, error) {
		db, err := tryOpen(mdDSN(extra))
		if err != nil {
			return nil, err
		}
		t.Cleanup(func() { _ = db.Close() })
		db.SetMaxOpenConns(1)
		if _, err := db.Exec("INSTALL motherduck"); err != nil {
			return nil, err
		}
		if _, err := db.Exec("LOAD motherduck"); err != nil {
			return nil, err
		}
		return db, nil
	}

	t.Run("secret TYPE motherduck then attach", func(t *testing.T) {
		db, err := open(t, "")
		require.NoError(t, err)
		_, err = db.Exec("CREATE TEMPORARY SECRET md (TYPE motherduck, TOKEN '" + tok + "')")
		t.Logf("CREATE SECRET: %v", err)
		require.NoError(t, err)
		_, err = db.Exec("ATTACH 'md:' AS mdall")
		t.Logf("ATTACH 'md:' (workspace mode) with secret only: %v", err)
		r, _ := rows(db, "SELECT database_name, type, path FROM duckdb_databases()")
		t.Logf("databases after workspace-mode attach: %v", r)
	})

	t.Run("token via SET after LOAD", func(t *testing.T) {
		db, err := open(t, "")
		require.NoError(t, err)
		_, err = db.Exec("SET motherduck_token='" + tok + "'")
		require.NoError(t, err)
		_, err = db.Exec("ATTACH 'md:?attach_mode=single' AS mdsingle")
		t.Logf("ATTACH 'md:?attach_mode=single': %v", err)
		r, _ := rows(db, "SELECT database_name, type, path FROM duckdb_databases()")
		t.Logf("databases after single-mode attach: %v", r)
		r, _ = rows(db, "SELECT name, value FROM duckdb_settings() WHERE name ILIKE '%motherduck_token%'")
		t.Logf("token visible via duckdb_settings(): %v", r)
	})

	t.Run("second token in the same process", func(t *testing.T) {
		db1, err := open(t, "")
		require.NoError(t, err)
		_, err = db1.Exec("SET motherduck_token='" + tok + "'")
		require.NoError(t, err)
		_, err = db1.Exec("ATTACH 'md:?attach_mode=single' AS a")
		require.NoError(t, err)
		db2, err := open(t, "")
		require.NoError(t, err)
		_, err = db2.Exec("SET motherduck_token='" + tok + "x'") // a different token
		t.Logf("second instance, different token: SET err=%v", err)
		_, err = db2.Exec("ATTACH 'md:?attach_mode=single' AS b")
		t.Logf("second instance ATTACH (different token): %v", err)
		// Same process, same instance: re-SET to another token then attach again.
		_, err = db1.Exec("SET motherduck_token='" + tok + "y'")
		t.Logf("re-SET token on live instance: %v", err)
	})
}
