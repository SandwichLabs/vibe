// Package buildinfo describes the running build for Settings > About and bug reports: the
// ldflags-stamped version, commit and date, plus the Go and DuckDB versions it was built with.
// It carries no workspace or user data.
package buildinfo

import (
	"database/sql"
	"runtime"

	_ "github.com/duckdb/duckdb-go/v2" // registers the "duckdb" driver for DuckDBVersion
)

// Info is what the About page shows and copies.
type Info struct {
	Version       string `json:"version"`
	Commit        string `json:"commit"`
	Date          string `json:"date"`
	GoVersion     string `json:"goVersion"`
	DuckDBVersion string `json:"duckdbVersion"`
	Platform      string `json:"platform"`
}

// Get combines the stamped values with what the runtime reports. The DuckDB version is read from
// a throwaway in-memory database; when that fails it is "unknown" rather than an error, since
// About must always render.
func Get(version, commit, date string) Info {
	return Info{
		Version:       version,
		Commit:        commit,
		Date:          date,
		GoVersion:     runtime.Version(),
		DuckDBVersion: duckdbVersion(),
		Platform:      runtime.GOOS + "/" + runtime.GOARCH,
	}
}

func duckdbVersion() string {
	db, err := sql.Open("duckdb", "")
	if err != nil {
		return "unknown"
	}
	defer db.Close()
	var v string
	if err := db.QueryRow("SELECT library_version FROM pragma_version()").Scan(&v); err != nil || v == "" {
		return "unknown"
	}
	return v
}
