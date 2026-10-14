package session

import (
	"net/url"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Spike #107 Q1: which hardening options are accepted as connector config at
// open (DSN query params) and which can be SET afterwards.
func TestSpikeHardeningDSN(t *testing.T) {
	scratch := t.TempDir()
	opts := map[string]string{
		"allow_persistent_secrets":     "false",
		"autoinstall_known_extensions": "false",
		"autoload_known_extensions":    "false",
		"allow_community_extensions":   "false",
		"temp_directory":               scratch,
		"lock_configuration":           "true",
	}
	for k, v := range opts {
		t.Run("dsn/"+k, func(t *testing.T) {
			db, err := tryOpen("?" + k + "=" + url.QueryEscape(v))
			require.NoError(t, err, "DSN param %s must be accepted", k)
			defer db.Close()
			t.Logf("%s => current_setting=%q", k, setting(t, db, k))
		})
	}
	// All at once, as the session would do.
	q := url.Values{}
	for k, v := range opts {
		q.Set(k, v)
	}
	db := openDSN(t, "?"+q.Encode())
	for k, v := range opts {
		assert.Equal(t, v, setting(t, db, k), k)
	}
	// lock_configuration => further SET fails.
	_, err := db.Exec("SET threads=1")
	require.Error(t, err)
	t.Logf("SET after lock_configuration: %v", err)
}

// Which options can be SET after open (before the secret manager is used)?
func TestSpikeHardeningSET(t *testing.T) {
	db := openDSN(t, "")
	for _, s := range []string{
		"SET allow_persistent_secrets=false",
		"SET autoinstall_known_extensions=false",
		"SET autoload_known_extensions=false",
		"SET allow_community_extensions=false",
		"SET temp_directory='" + t.TempDir() + "'",
	} {
		_, err := db.Exec(s)
		t.Logf("%-70s err=%v", s, err)
		require.NoError(t, err, s)
	}
	assert.Equal(t, "false", setting(t, db, "allow_persistent_secrets"))
	assert.Equal(t, "false", setting(t, db, "allow_community_extensions"))
}

// What fails once the secret manager has been used (CREATE SECRET)?
func TestSpikeHardeningAfterSecretManager(t *testing.T) {
	db := openDSN(t, "")
	_, err := db.Exec("CREATE TEMPORARY SECRET s0 (TYPE http, BEARER_TOKEN 'x')")
	require.NoError(t, err)
	for _, s := range []string{
		"SET allow_persistent_secrets=false",
		"SET allow_persistent_secrets=true",
		"SET autoinstall_known_extensions=false",
		"SET autoload_known_extensions=false",
		"SET allow_community_extensions=false",
		"SET temp_directory='" + t.TempDir() + "'",
		"SET lock_configuration=true",
	} {
		_, err := db.Exec(s)
		t.Logf("after secret manager: %-60s err=%v", s, err)
	}
	// Default allow_persistent_secrets is true: a persistent secret is written to
	// secret_directory. DuckDB 1.5 honours secret_directory in the DSN (1.4.1
	// silently ignored it, hence the SET in production); the default is
	// ~/.duckdb/stored_secrets, never touched here.
	secDir := t.TempDir()
	db2 := openDSN(t, "?secret_directory="+url.QueryEscape(secDir))
	assert.Equal(t, secDir, setting(t, db2, "secret_directory"), "DSN secret_directory is honoured on DuckDB 1.5 (it was silently ignored on 1.4.1)")
	_, err = db2.Exec("SET secret_directory='" + secDir + "'")
	require.NoError(t, err)
	assert.Equal(t, secDir, setting(t, db2, "secret_directory"))
	_, err = db2.Exec("CREATE PERSISTENT SECRET ps (TYPE http, BEARER_TOKEN 'x')")
	require.NoError(t, err)
	files, _ := filepath.Glob(filepath.Join(secDir, "*.duckdb_secret"))
	assert.NotEmpty(t, files, "persistent secret file lands in secret_directory")
	t.Logf("persistent secret files: %v", files)
	db3 := openDSN(t, "?allow_persistent_secrets=false")
	_, err = db3.Exec("SET secret_directory='" + t.TempDir() + "'")
	require.NoError(t, err)
	_, err = db3.Exec("CREATE PERSISTENT SECRET ps (TYPE http, BEARER_TOKEN 'x')")
	require.Error(t, err)
	t.Logf("CREATE PERSISTENT SECRET with allow_persistent_secrets=false: %v", err)
}

// Reviewer claim: lock_configuration does NOT block ATTACH / CREATE SECRET.
func TestSpikeLockConfigurationDoesNotBlockAttachOrSecret(t *testing.T) {
	db := openDSN(t, "?allow_persistent_secrets=false&lock_configuration=true")
	db.SetMaxOpenConns(1)
	_, err := db.Exec("SET threads=1")
	require.Error(t, err, "SET must be blocked")
	_, err = db.Exec("PRAGMA threads=1")
	require.Error(t, err, "PRAGMA write must be blocked")
	_, err = db.Exec("CREATE TEMPORARY SECRET s1 (TYPE http, BEARER_TOKEN 'x')")
	assert.NoError(t, err, "CREATE SECRET is not blocked by lock_configuration")
	_, err = db.Exec("ATTACH '" + t.TempDir() + "/a.duckdb' AS a")
	assert.NoError(t, err, "ATTACH is not blocked by lock_configuration")
	_, err = db.Exec("DETACH a")
	assert.NoError(t, err)
	_, err = db.Exec("INSTALL json")
	t.Logf("INSTALL after lock: %v", err)
	_, err = db.Exec("LOAD json")
	t.Logf("LOAD json after lock: %v", err)
	_, err = db.Exec("SET VARIABLE v = 1")
	t.Logf("SET VARIABLE after lock: %v", err)
	_, err = db.Exec("USE memory")
	t.Logf("USE after lock: %v", err)
	_, err = db.Exec("DROP SECRET s1")
	t.Logf("DROP SECRET after lock: %v", err)
}

// ATTACH/secret are instance-level, USE is per connection (confirms §4.4 intro).
func TestSpikeInstanceVsConnectionScope(t *testing.T) {
	db := openDSN(t, "")
	db.SetMaxOpenConns(2)
	c1, err := db.Conn(t.Context())
	require.NoError(t, err)
	defer c1.Close()
	c2, err := db.Conn(t.Context())
	require.NoError(t, err)
	defer c2.Close()
	_, err = c1.ExecContext(t.Context(), "ATTACH '"+t.TempDir()+"/x.duckdb' AS x")
	require.NoError(t, err)
	_, err = c1.ExecContext(t.Context(), "CREATE TEMPORARY SECRET s (TYPE http, BEARER_TOKEN 'x')")
	require.NoError(t, err)
	_, err = c1.ExecContext(t.Context(), "USE x")
	require.NoError(t, err)
	v, _ := scalar(t.Context(), c2, "SELECT count(*) FROM duckdb_databases() WHERE database_name='x'")
	assert.Equal(t, "1", v, "ATTACH visible from second conn")
	v, _ = scalar(t.Context(), c2, "SELECT count(*) FROM duckdb_secrets() WHERE name='s'")
	assert.Equal(t, "1", v, "secret visible from second conn")
	v, _ = scalar(t.Context(), c2, "current_database()")
	assert.Equal(t, "memory", v, "USE is per connection")
}
