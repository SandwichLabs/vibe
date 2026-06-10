package container

// Auto-number backfill (Access AutoNumber parity). Tables created before the
// sequence-backed-PK fix carry a bare "id BIGINT PRIMARY KEY" — a data-entry
// form that omits the id then fails DuckDB's NOT NULL constraint, because
// DuckDB has no implicit AUTOINCREMENT. On every Open we retrofit such tables:
// each single-column integer primary key with no DEFAULT gets a
// _vibe_seq_<table>_<column> sequence (seeded past the current max id) wired
// in via ALTER COLUMN ... SET DEFAULT. Idempotent and best-effort: a failure
// is logged, never blocks opening the file.

import (
	"database/sql"
	"fmt"
	"log/slog"
	"strings"

	"github.com/sandwich-labs/vibe-app/internal/schema"
)

// integerPKTypes is the integer family eligible for auto-numbering.
func isIntegerPKType(t string) bool {
	switch strings.ToUpper(strings.TrimSpace(t)) {
	case "BIGINT", "INTEGER", "INT", "INT2", "INT4", "INT8", "SMALLINT", "TINYINT",
		"HUGEINT", "UBIGINT", "UINTEGER", "USMALLINT", "UTINYINT":
		return true
	}
	return false
}

// backfillAutoNumberPKs retrofits sequence DEFAULTs onto bare integer primary
// keys in the user's DuckDB data.
func backfillAutoNumberPKs(ddb *sql.DB, logger *slog.Logger) {
	rows, err := ddb.Query(
		`SELECT table_name, constraint_column_names
		   FROM duckdb_constraints()
		  WHERE constraint_type = 'PRIMARY KEY' AND schema_name = 'main'`)
	if err != nil {
		logger.Warn("auto-number backfill: failed to list primary keys", "error", err)
		return
	}
	type pk struct{ table, column string }
	var candidates []pk
	for rows.Next() {
		var table string
		var names []any
		if err := rows.Scan(&table, &names); err != nil {
			logger.Warn("auto-number backfill: failed to scan constraint", "error", err)
			continue
		}
		// Only single-column PKs auto-number (composite keys are user-managed).
		if len(names) != 1 {
			continue
		}
		col, ok := names[0].(string)
		if !ok {
			continue
		}
		candidates = append(candidates, pk{table: table, column: col})
	}
	if err := rows.Err(); err != nil {
		logger.Warn("auto-number backfill: failed to iterate constraints", "error", err)
	}
	_ = rows.Close() //nolint:errcheck

	for _, c := range candidates {
		if !schema.ValidIdentifier(c.table) || !schema.ValidIdentifier(c.column) {
			continue
		}
		var dataType string
		var hasDefault bool
		err := ddb.QueryRow(
			`SELECT data_type, column_default IS NOT NULL
			   FROM information_schema.columns
			  WHERE table_schema = 'main' AND table_name = $1 AND column_name = $2`,
			c.table, c.column,
		).Scan(&dataType, &hasDefault)
		if err != nil {
			logger.Warn("auto-number backfill: failed to inspect column",
				"table", c.table, "column", c.column, "error", err)
			continue
		}
		if hasDefault || !isIntegerPKType(dataType) {
			continue
		}

		// Seed the sequence past the existing rows so backfilled tables keep
		// numbering where their data left off.
		var next int64
		if err := ddb.QueryRow(
			fmt.Sprintf("SELECT COALESCE(MAX(%q), 0) + 1 FROM %q", c.column, c.table),
		).Scan(&next); err != nil {
			logger.Warn("auto-number backfill: failed to read max id",
				"table", c.table, "error", err)
			continue
		}

		seq := fmt.Sprintf("_vibe_seq_%s_%s", c.table, c.column)
		if _, err := ddb.Exec(fmt.Sprintf("CREATE SEQUENCE IF NOT EXISTS %q START %d", seq, next)); err != nil {
			logger.Warn("auto-number backfill: failed to create sequence",
				"table", c.table, "sequence", seq, "error", err)
			continue
		}
		if _, err := ddb.Exec(fmt.Sprintf(
			"ALTER TABLE %q ALTER COLUMN %q SET DEFAULT nextval('%s')", c.table, c.column, seq,
		)); err != nil {
			logger.Warn("auto-number backfill: failed to set default",
				"table", c.table, "column", c.column, "error", err)
			continue
		}
		logger.Info("auto-number backfill: retrofitted primary key",
			"table", c.table, "column", c.column, "sequence", seq, "start", next)
	}
}
