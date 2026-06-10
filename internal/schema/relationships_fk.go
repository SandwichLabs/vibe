package schema

// Foreign-key emit logic (plan §2.1/§2.3/§4.5). DuckDB cannot ALTER a foreign
// key onto an existing table (see internal/engine relationship spike §8/§4.1),
// so a FK is applied by recreating the child table with the constraint declared
// inline (copy rows into a rebuilt table that carries the inline REFERENCES,
// then swap). DuckDB FKs are effectively RESTRICT-only, which matches Access's
// "enforce referential integrity" default.
//
// This is the single reusable FK-emit path shared by the Studio backend
// (studio/app.go AddForeignKey) and the agent Schema Wizard tool
// (internal/agents create_relationship), so both apply identical DDL and
// persist identical _vibe_relationships metadata.

import (
	"database/sql"
	"fmt"
	"strings"
)

// ForeignKeySpec describes a foreign-key relationship to apply: the child column
// references the parent column, with optional cascade options. (Cascade options
// are persisted as metadata; DuckDB enforces RESTRICT regardless.)
type ForeignKeySpec struct {
	ChildTable   string
	ChildColumn  string
	ParentTable  string
	ParentColumn string
	OnDelete     string
	OnUpdate     string
}

// FKColumn describes a column for table reconstruction. Default carries the
// column's DEFAULT expression verbatim (e.g. an auto-number PK's
// "nextval('_vibe_seq_orders_id')") so a rebuild does not strip it.
type FKColumn struct {
	Name    string
	Type    string
	NotNull bool
	PK      bool
	Default string
}

// ApplyForeignKey applies a real DuckDB foreign-key constraint for spec by
// recreating the child table with the FK declared inline. ddb is the DuckDB
// connection (user data); all validation runs BEFORE any mutation so a rejected
// call leaves the schema untouched.
func ApplyForeignKey(ddb *sql.DB, spec ForeignKeySpec) error {
	for _, id := range []string{spec.ChildTable, spec.ChildColumn, spec.ParentTable, spec.ParentColumn} {
		if !ValidIdentifier(id) {
			return fmt.Errorf("invalid identifier %q in foreign key spec", id)
		}
	}

	childCols, err := TableColumns(ddb, spec.ChildTable)
	if err != nil {
		return fmt.Errorf("failed to inspect child table %q: %w", spec.ChildTable, err)
	}
	if len(childCols) == 0 {
		return fmt.Errorf("child table %q does not exist", spec.ChildTable)
	}
	if !columnExists(childCols, spec.ChildColumn) {
		return fmt.Errorf("child column %q does not exist on table %q", spec.ChildColumn, spec.ChildTable)
	}

	parentCols, err := TableColumns(ddb, spec.ParentTable)
	if err != nil {
		return fmt.Errorf("failed to inspect parent table %q: %w", spec.ParentTable, err)
	}
	if len(parentCols) == 0 {
		return fmt.Errorf("parent table %q does not exist", spec.ParentTable)
	}
	if !columnExists(parentCols, spec.ParentColumn) {
		return fmt.Errorf("parent column %q does not exist on table %q", spec.ParentColumn, spec.ParentTable)
	}

	// DuckDB requires the referenced (parent) column to be UNIQUE or PRIMARY KEY.
	unique, err := columnIsUnique(ddb, spec.ParentTable, spec.ParentColumn)
	if err != nil {
		return fmt.Errorf("failed to inspect parent constraints on %q: %w", spec.ParentTable, err)
	}
	if !unique {
		return fmt.Errorf("referenced column %q.%q must be a PRIMARY KEY or UNIQUE column", spec.ParentTable, spec.ParentColumn)
	}

	if err := rebuildTableWithFK(ddb, spec, childCols); err != nil {
		return fmt.Errorf("failed to apply foreign key to %q: %w", spec.ChildTable, err)
	}
	return nil
}

// StripForeignKeys rebuilds the child table with no inline REFERENCES,
// removing its foreign-key constraint(s). DuckDB cannot ALTER a FK off an
// existing table, so removal is a rebuild, mirroring ApplyForeignKey. A
// missing table is a no-op.
func StripForeignKeys(ddb *sql.DB, childTable string) error {
	if !ValidIdentifier(childTable) {
		return fmt.Errorf("invalid table name %q", childTable)
	}
	cols, err := TableColumns(ddb, childTable)
	if err != nil {
		return fmt.Errorf("failed to inspect child table %q: %w", childTable, err)
	}
	if len(cols) == 0 {
		return nil
	}
	if err := rebuildTableWithFK(ddb, ForeignKeySpec{ChildTable: childTable}, cols); err != nil {
		return fmt.Errorf("failed to drop foreign key from %q: %w", childTable, err)
	}
	return nil
}

// TableColumns introspects a DuckDB table's columns (name, type, nullability,
// primary-key membership) in ordinal order. Returns an empty slice if the table
// does not exist.
func TableColumns(db *sql.DB, table string) ([]FKColumn, error) {
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

	var cols []FKColumn
	for rows.Next() {
		var c FKColumn
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
	for i := range cols {
		if pk[cols[i].Name] {
			cols[i].PK = true
		}
	}
	return cols, nil
}

// columnIsUnique reports whether the given column is covered by a single-column
// PRIMARY KEY or UNIQUE constraint (DuckDB's requirement for a FK target).
func columnIsUnique(db *sql.DB, table, column string) (bool, error) {
	for _, ct := range []string{"PRIMARY KEY", "UNIQUE"} {
		cols, err := constraintColumns(db, table, ct)
		if err != nil {
			return false, err
		}
		if cols[column] {
			return true, nil
		}
	}
	return false, nil
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

// rebuildTableWithFK recreates a child table with the same columns, preserving
// types / NOT NULL / PRIMARY KEY, optionally adding an inline FK REFERENCES on
// spec.ChildColumn (when spec.ParentTable is set). Existing rows are copied into
// the rebuilt table.
//
// CRITICAL: the rebuilt child table is created directly under its FINAL name
// (the original is renamed aside to a temp first, then dropped) — it is NOT
// created under a temp name and renamed into place. DuckDB records a FK as a
// dependency on the PARENT table keyed by the CHILD table's name AT CREATE TIME,
// and a subsequent ALTER ... RENAME of the child does NOT rewrite that recorded
// dependency. Creating the child as "orders__fk_rebuild" then renaming it to
// "orders" therefore left the parent ("customers") with a dependency pointing at
// the now-dropped "orders__fk_rebuild", which bricked ALL deletes on the parent
// (even unreferenced rows) with a "Table ... does not exist" catalog error.
// Building under the final name keeps the parent's FK dependency graph correct,
// so RESTRICT blocks only referenced-parent deletes and unreferenced-parent
// deletes succeed. Done in a single transaction so a failure (e.g. an FK that
// orphan rows would violate) leaves the original table untouched.
func rebuildTableWithFK(db *sql.DB, spec ForeignKeySpec, cols []FKColumn) error {
	stash := spec.ChildTable + "__fk_rebuild"

	var defs []string
	for _, c := range cols {
		def := fmt.Sprintf("%q %s", c.Name, c.Type)
		if c.Default != "" {
			// Preserve the DEFAULT expression (notably auto-number sequences) —
			// rebuilding without it silently broke id-less form inserts.
			def += " DEFAULT " + c.Default
		}
		if c.PK {
			def += " PRIMARY KEY"
		}
		if c.NotNull && !c.PK {
			def += " NOT NULL"
		}
		if spec.ParentTable != "" && c.Name == spec.ChildColumn {
			def += fmt.Sprintf(" REFERENCES %q(%q)", spec.ParentTable, spec.ParentColumn)
		}
		defs = append(defs, def)
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin rebuild transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Drop any stale stash, then move the original child aside under the stash
	// name. The rebuilt table is created under the ORIGINAL (final) name so the
	// parent's recorded FK dependency points at the correct, surviving table.
	if _, err := tx.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %q", stash)); err != nil {
		return fmt.Errorf("failed to drop stale rebuild table: %w", err)
	}
	if _, err := tx.Exec(fmt.Sprintf("ALTER TABLE %q RENAME TO %q", spec.ChildTable, stash)); err != nil {
		return fmt.Errorf("failed to stash original table: %w", err)
	}
	if _, err := tx.Exec(fmt.Sprintf("CREATE TABLE %q (%s)", spec.ChildTable, strings.Join(defs, ", "))); err != nil {
		return fmt.Errorf("failed to create rebuilt table: %w", err)
	}
	if _, err := tx.Exec(fmt.Sprintf("INSERT INTO %q SELECT * FROM %q", spec.ChildTable, stash)); err != nil {
		return fmt.Errorf("failed to copy rows into rebuilt table: %w", err)
	}
	if _, err := tx.Exec(fmt.Sprintf("DROP TABLE %q", stash)); err != nil {
		return fmt.Errorf("failed to drop stashed original table: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit table rebuild: %w", err)
	}
	return nil
}

// columnExists reports whether a column of the given name is present.
func columnExists(cols []FKColumn, name string) bool {
	for _, c := range cols {
		if c.Name == name {
			return true
		}
	}
	return false
}

// NormalizeAction normalizes a referential action. DuckDB FKs are effectively
// RESTRICT-only; any unrecognized/empty value is stored as the Access default
// "RESTRICT" so the persisted metadata reflects the enforced behavior.
func NormalizeAction(action string) string {
	switch strings.ToUpper(strings.TrimSpace(action)) {
	case "CASCADE":
		return "CASCADE"
	case "SET NULL":
		return "SET NULL"
	case "NO ACTION":
		return "NO ACTION"
	default:
		return "RESTRICT"
	}
}

// ValidIdentifier reports whether name is a safe SQL identifier (letters,
// digits, underscores; not starting with a digit). Used to gate identifiers
// that are interpolated into DDL/DML.
func ValidIdentifier(name string) bool {
	if name == "" {
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
