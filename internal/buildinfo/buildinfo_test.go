package buildinfo

import (
	"database/sql"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetCarriesStampedValuesAndRuntime(t *testing.T) {
	got := Get("v1.2.3", "abc1234", "2026-10-12T00:00:00Z")
	assert.Equal(t, "v1.2.3", got.Version)
	assert.Equal(t, "abc1234", got.Commit)
	assert.Equal(t, "2026-10-12T00:00:00Z", got.Date)
	assert.Equal(t, runtime.Version(), got.GoVersion)
	assert.Equal(t, runtime.GOOS+"/"+runtime.GOARCH, got.Platform)
	require.NotEmpty(t, got.DuckDBVersion)
	assert.Regexp(t, `^v\d+\.\d+`, got.DuckDBVersion)
}

// The TPC-H and TPC-DS generators carry the non-permissive TPC EULA. The bindings name their archives on the link
// line, but the linker drops them while nothing references them. If a future DuckDB or binding statically links
// them, scripts/notices/duckdb-third-party.md and the licence review must change before this test is relaxed.
func TestTPCExtensionsAreNotLinked(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer db.Close()
	var n int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM duckdb_extensions() WHERE extension_name IN ('tpch','tpcds') AND install_mode = 'STATICALLY_LINKED'`).Scan(&n))
	assert.Zero(t, n, "tpch/tpcds became statically linked: their TPC EULA needs a licence review")
}
