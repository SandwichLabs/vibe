package engine

// Table/column introspection and identifier validation shared by the CRUD
// write core, the Query Wizard agent tool, and the container's auto-number
// backfill. Ported from the (now-removed) internal/schema package's
// relationships_fk.go — this codebase's Access-application layer is gone
// (#68), but these helpers are generic DuckDB introspection, not
// forms/relationships-UI specific, so they moved here instead of being
// deleted.

import (
	"database/sql"
	"fmt"
	"strings"
)

// ColumnInfo describes a table column (name, type, nullability, primary-key
// and foreign-key membership, default expression) in ordinal order.
type ColumnInfo struct {
	Name    string
	Type    string
	NotNull bool
	PK      bool
	// FK is true when the column participates in a FOREIGN KEY constraint
	// (a plain-SQL inline REFERENCES — #68 dropped the _vibe_relationships
	// metadata table this used to be read from). Read straight from
	// DuckDB's own duckdb_constraints() catalog, so it reflects reality
	// regardless of how the FK was declared.
	FK      bool
	Default string
}

// TableColumns introspects a DuckDB table's columns (name, type, nullability,
// primary-key membership, default expression) in ordinal order. Returns an
// empty slice if the table does not exist.
func TableColumns(db *sql.DB, table string) ([]ColumnInfo, error) {
	rows, err := db.Query(
		`SELECT column_name, data_type, CASE WHEN is_nullable = 'NO' THEN true ELSE false END,
		        COALESCE(column_default, '')
		   FROM information_schema.columns
		  WHERE table_schema = 'main' AND table_name = $1
		  ORDER BY ordinal_position`, table)
	if err != nil {
		return nil, fmt.Errorf("failed to query columns: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var cols []ColumnInfo
	for rows.Next() {
		var c ColumnInfo
		if err := rows.Scan(&c.Name, &c.Type, &c.NotNull, &c.Default); err != nil {
			return nil, fmt.Errorf("failed to scan column: %w", err)
		}
		cols = append(cols, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate columns: %w", err)
	}

	pk, err := constraintColumns(db, table, "PRIMARY KEY")
	if err != nil {
		return nil, err
	}
	fk, err := constraintColumns(db, table, "FOREIGN KEY")
	if err != nil {
		return nil, err
	}
	for i := range cols {
		if pk[cols[i].Name] {
			cols[i].PK = true
		}
		if fk[cols[i].Name] {
			cols[i].FK = true
		}
	}
	return cols, nil
}

// constraintColumns returns the set of column names participating in any
// constraint of the given type on the table, using DuckDB's duckdb_constraints()
// table function.
func constraintColumns(db *sql.DB, table, constraintType string) (map[string]bool, error) {
	rows, err := db.Query(
		`SELECT constraint_column_names FROM duckdb_constraints()
		  WHERE table_name = $1 AND constraint_type = $2`, table, constraintType)
	if err != nil {
		return nil, fmt.Errorf("failed to query %s constraints: %w", constraintType, err)
	}
	defer rows.Close() //nolint:errcheck

	set := map[string]bool{}
	for rows.Next() {
		var names []any
		if err := rows.Scan(&names); err != nil {
			return nil, fmt.Errorf("failed to scan constraint columns: %w", err)
		}
		for _, n := range names {
			if s, ok := n.(string); ok {
				set[s] = true
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate constraints: %w", err)
	}
	return set, nil
}

// ValidIdentifier reports whether name is a safe SQL identifier (letters,
// digits, underscores; not starting with a digit) naming a table/column a
// caller may create, read, or write — this is the ONE identifier validator
// for the whole codebase (studio's own isValidIdentifier is a thin wrapper
// around it, not a second implementation) used to gate identifiers that are
// interpolated into DDL/DML.
//
// The "_vibe_" prefix is always rejected: those names are reserved for this
// app's own internal tables (_vibe_meta, _vibe_skills, _vibe_import_stage,
// ...), and a caller validating a target for its own read/write must never
// be allowed to touch one, whether by a crafted table name in a shared file
// or an import/create-table request that happens to collide with the
// prefix.
func ValidIdentifier(name string) bool {
	if name == "" || strings.HasPrefix(name, "_vibe_") {
		return false
	}
	for i, r := range name {
		isLetter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_'
		isDigit := r >= '0' && r <= '9'
		if i == 0 && !isLetter {
			return false
		}
		if !isLetter && !isDigit {
			return false
		}
	}
	return true
}
