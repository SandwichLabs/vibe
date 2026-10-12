package session

import (
	"database/sql"
	"fmt"
	"strings"
)

// CopyFormat returns the file extension and the COPY options for an export
// format, "csv" (with a header row) or "parquet"; case and surrounding space do
// not matter.
func CopyFormat(format string) (ext, opts string, err error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "csv":
		return ".csv", "FORMAT csv, HEADER true", nil
	case "parquet":
		return ".parquet", "FORMAT parquet", nil
	}
	return "", "", fmt.Errorf("unsupported export format %q (want csv or parquet)", format)
}

// CopyQueryTo writes the rows of query to the file dst with COPY ... TO (see
// CopyToSQL for how query and dst are quoted). opts is caller-built option text
// (CopyFormat), never user input. The caller has checked that query is a single
// SELECT where that matters.
func CopyQueryTo(db *sql.DB, query, dst, opts string) error {
	_, err := db.Exec(CopyToSQL(query, dst, opts))
	return err
}
