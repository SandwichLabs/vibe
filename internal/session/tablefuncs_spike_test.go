package session

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Spike #107 agent-policy inputs: which table functions / syntaxes read the
// local filesystem or a URL. Offline-safe (no INSTALL).
func TestSpikeFileReadingSurfaces(t *testing.T) {
	dir := t.TempDir()
	csv := filepath.Join(dir, "d.csv")
	require.NoError(t, os.WriteFile(csv, []byte("a,b\n1,2\n"), 0o600))
	js := filepath.Join(dir, "d.json")
	require.NoError(t, os.WriteFile(js, []byte(`{"a":1}`+"\n"), 0o600))
	txt := filepath.Join(dir, "d.txt")
	require.NoError(t, os.WriteFile(txt, []byte("hello"), 0o600))
	pq := filepath.Join(dir, "d.parquet")
	seed := openDSN(t, "")
	_, err := seed.Exec(fmt.Sprintf("COPY (SELECT 1 AS a) TO '%s' (FORMAT parquet)", pq))
	require.NoError(t, err)
	dbf := filepath.Join(dir, "x.duckdb")
	_, err = seed.Exec(fmt.Sprintf("ATTACH '%s' AS xx; CREATE TABLE xx.t AS SELECT 1 a; DETACH xx", dbf))
	require.NoError(t, err)

	db := openDSN(t, "")
	probes := []struct{ name, q string }{
		{"read_csv", fmt.Sprintf("SELECT * FROM read_csv('%s')", csv)},
		{"read_csv_auto", fmt.Sprintf("SELECT * FROM read_csv_auto('%s')", csv)},
		{"sniff_csv", fmt.Sprintf("SELECT * FROM sniff_csv('%s')", csv)},
		{"read_text", fmt.Sprintf("SELECT * FROM read_text('%s')", txt)},
		{"read_blob", fmt.Sprintf("SELECT * FROM read_blob('%s')", txt)},
		{"read_json", fmt.Sprintf("SELECT * FROM read_json('%s')", js)},
		{"read_json_auto", fmt.Sprintf("SELECT * FROM read_json_auto('%s')", js)},
		{"read_json_objects", fmt.Sprintf("SELECT * FROM read_json_objects('%s')", js)},
		{"read_ndjson", fmt.Sprintf("SELECT * FROM read_ndjson('%s')", js)},
		{"read_parquet", fmt.Sprintf("SELECT * FROM read_parquet('%s')", pq)},
		{"parquet_scan", fmt.Sprintf("SELECT * FROM parquet_scan('%s')", pq)},
		{"parquet_metadata", fmt.Sprintf("SELECT * FROM parquet_metadata('%s')", pq)},
		{"parquet_schema", fmt.Sprintf("SELECT * FROM parquet_schema('%s')", pq)},
		{"parquet_file_metadata", fmt.Sprintf("SELECT * FROM parquet_file_metadata('%s')", pq)},
		{"parquet_kv_metadata", fmt.Sprintf("SELECT * FROM parquet_kv_metadata('%s')", pq)},
		{"glob", fmt.Sprintf("SELECT * FROM glob('%s/*')", dir)},
		{"glob (outside any dir)", "SELECT count(*) FROM glob('/etc/*')"},
		{"replacement scan: FROM 'file.csv'", fmt.Sprintf("SELECT * FROM '%s'", csv)},
		{"replacement scan: FROM '/etc/hostname'?", "SELECT * FROM '/etc/passwd'"},
		{"replacement scan: parquet", fmt.Sprintf("SELECT * FROM '%s'", pq)},
		{"replacement scan: .duckdb file", fmt.Sprintf("SELECT * FROM '%s'", dbf)},
		{"ATTACH file", fmt.Sprintf("ATTACH '%s' AS att (READ_ONLY)", dbf)},
		{"COPY FROM", fmt.Sprintf("CREATE TEMP TABLE cp(a int,b int); COPY cp FROM '%s' (HEADER)", csv)},
		{"COPY TO", fmt.Sprintf("COPY (SELECT 1) TO '%s/out.csv'", dir)},
		{"EXPORT DATABASE", fmt.Sprintf("EXPORT DATABASE '%s/exp'", dir)},
		{"read_text via CTE", fmt.Sprintf("WITH c AS (SELECT content FROM read_text('%s')) SELECT * FROM c", txt)},
		{"read_text via macro", fmt.Sprintf("CREATE TEMP MACRO leak() AS TABLE SELECT * FROM read_text('%s'); SELECT * FROM leak()", txt)},
		{"read_text via view", fmt.Sprintf("CREATE TEMP VIEW leakv AS SELECT * FROM read_text('%s'); SELECT * FROM leakv", txt)},
		{"read_text file:// URL", "SELECT * FROM read_text('file://" + txt + "')"},
		{"getenv", "SELECT getenv('HOME')"},
		{"current_setting home_directory", "SELECT current_setting('home_directory')"},
		{"duckdb_settings()", "SELECT count(*) FROM duckdb_settings()"},
		{"duckdb_extensions()", "SELECT extension_name, loaded, installed FROM duckdb_extensions() WHERE loaded"},
	}
	for _, p := range probes {
		_, err := rows(db, p.q)
		if err != nil {
			t.Logf("BLOCKED/ERR %-40s %v", p.name, firstLine(err))
			continue
		}
		t.Logf("READS      %-40s ok", p.name)
	}
	// Hard assertions on the things the policy must handle.
	for _, name := range []string{"read_csv", "read_text", "read_blob", "read_json", "read_parquet", "glob"} {
		for _, p := range probes {
			if p.name == name {
				_, err := rows(db, p.q)
				assert.NoError(t, err, name)
			}
		}
	}
	_, err = rows(db, "SELECT * FROM '/etc/passwd'")
	t.Logf("replacement scan of arbitrary path: err=%v", err)

	// Inventory of every table function; the allow-list (§4.5) is the complement
	// of this list minus the known-safe ones.
	all, err := rows(db, "SELECT DISTINCT function_name FROM duckdb_functions() WHERE function_type='table' AND NOT internal ORDER BY 1")
	require.NoError(t, err)
	all2, err := rows(db, "SELECT DISTINCT function_name FROM duckdb_functions() WHERE function_type='table' ORDER BY 1")
	require.NoError(t, err)
	t.Logf("table functions (all, %d): %s", len(all2), strings.Join(all2, ","))
	t.Logf("table functions (non-internal, %d)", len(all))
}

func firstLine(err error) string {
	s := err.Error()
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

// With enable_external_access=false (+ allowed_directories), what is blocked?
func TestSpikeExternalAccessOffAndAllowedDirectories(t *testing.T) {
	allowed := t.TempDir()
	outside := t.TempDir()
	ok := filepath.Join(allowed, "ok.csv")
	no := filepath.Join(outside, "no.csv")
	require.NoError(t, os.WriteFile(ok, []byte("a\n1\n"), 0o600))
	require.NoError(t, os.WriteFile(no, []byte("a\n2\n"), 0o600))

	// allowed_directories is a LIST: not expressible in the DSN (string-only
	// config), so it needs SET after open; enable_external_access=false is accepted in the DSN.
	_, err := tryOpen("?allowed_directories=" + url.QueryEscape("['"+allowed+"']"))
	t.Logf("allowed_directories via DSN: err=%v", err)
	require.Error(t, err)
	db := openDSN(t, "?enable_external_access=false")
	db.SetMaxOpenConns(1)
	_, err = db.Exec("SET allowed_directories=['" + allowed + "']")
	t.Logf("SET allowed_directories after enable_external_access=false in DSN: %v", err)
	if err != nil {
		// Order matters: allowed_directories first, then disable external access.
		db = openDSN(t, "")
		db.SetMaxOpenConns(1)
		_, err = db.Exec("SET allowed_directories=['" + allowed + "']")
		require.NoError(t, err)
		_, err = db.Exec("SET enable_external_access=false")
		require.NoError(t, err)
	}
	t.Logf("enable_external_access=%s allowed_directories=%s", setting(t, db, "enable_external_access"), setting(t, db, "allowed_directories"))

	for _, p := range []struct{ name, q string }{
		{"read_csv inside allowed", fmt.Sprintf("SELECT * FROM read_csv('%s')", ok)},
		{"read_csv outside allowed", fmt.Sprintf("SELECT * FROM read_csv('%s')", no)},
		{"read_text outside", fmt.Sprintf("SELECT * FROM read_text('%s')", no)},
		{"glob inside", fmt.Sprintf("SELECT * FROM glob('%s/*')", allowed)},
		{"glob outside", fmt.Sprintf("SELECT * FROM glob('%s/*')", outside)},
		{"replacement scan inside", fmt.Sprintf("SELECT * FROM '%s'", ok)},
		{"replacement scan outside", fmt.Sprintf("SELECT * FROM '%s'", no)},
		{"COPY TO inside", fmt.Sprintf("COPY (SELECT 1) TO '%s/o.csv'", allowed)},
		{"COPY TO outside", fmt.Sprintf("COPY (SELECT 1) TO '%s/o.csv'", outside)},
		{"ATTACH db inside", fmt.Sprintf("ATTACH '%s/a.duckdb' AS ai", allowed)},
		{"ATTACH db outside", fmt.Sprintf("ATTACH '%s/a.duckdb' AS ao", outside)},
		{"../ traversal", fmt.Sprintf("SELECT * FROM read_csv('%s/../%s/no.csv')", allowed, filepath.Base(outside))},
		{"SET enable_external_access=true", "SET enable_external_access=true"},
		{"SET allowed_directories=[]", "SET allowed_directories=['/']"},
		{"INSTALL json", "INSTALL json"},
		{"LOAD json", "LOAD json"},
		{"CREATE TEMPORARY SECRET", "CREATE TEMPORARY SECRET s (TYPE s3, KEY_ID 'k', SECRET 'x')"},
		{"ATTACH :memory:", "ATTACH ':memory:' AS m2"},
		{"temp spill dir still usable", "SELECT count(*) FROM range(1000000) a, range(10) b"},
	} {
		_, err := rows(db, p.q)
		if err != nil {
			t.Logf("BLOCKED %-34s %v", p.name, firstLine(err))
		} else {
			t.Logf("allowed %-34s", p.name)
		}
	}
	// Hard assertions: the sandbox must hold.
	_, err = rows(db, fmt.Sprintf("SELECT * FROM read_csv('%s')", no))
	require.Error(t, err)
	_, err = rows(db, fmt.Sprintf("SELECT * FROM read_csv('%s')", ok))
	require.NoError(t, err)
	_, err = rows(db, "SET enable_external_access=true")
	require.Error(t, err, "irreversible after open")
}
