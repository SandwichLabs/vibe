package session

import (
	"context"
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
// CopyToSQL for how query and dst are quoted). query may carry positional ?
// placeholders (engine.BindParams) with their values in args; they are bound by
// the driver, never spliced into the text. opts is caller-built option text
// (CopyFormat), never user input. The caller has checked that query is a single
// SELECT where that matters.
func CopyQueryTo(ctx context.Context, db *sql.DB, query string, args []any, dst, opts string) error {
	_, err := db.ExecContext(ctx, CopyToSQL(query, dst, opts), args...)
	return err
}
