package crud

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/session"
)

// ColumnDef is one column of a table to create. Type is a DuckDB type
// expression that cannot be quoted, so it is checked with ValidColumnType before
// it reaches the DDL; the name, table and sequence are quoted.
type ColumnDef struct {
	Name    string
	Type    string
	NotNull bool
	PK      bool
}

// CreateTable creates ref with the given columns. An integer primary-key column
// is made auto-numbering via a backing sequence (the Access AutoNumber
// behavior) created next to the table, so data-entry forms can omit the id.
// bareDefault says ref is in the session's default catalog and schema, where a
// bare nextval('seq') resolves (the legacy default text); otherwise the
// sequence is referenced by its quoted, fully qualified name. Column names must
// satisfy engine.ValidIdentifier.
func CreateTable(ctx context.Context, db *sql.DB, ref session.TableRef, cols []ColumnDef, bareDefault bool) error {
	for _, col := range cols {
		if !engine.ValidIdentifier(col.Name) {
			return fmt.Errorf("invalid column name %q", col.Name)
		}
		if !ValidColumnType(col.Type) {
			return fmt.Errorf("invalid column type %q: use letters, digits, spaces, parentheses and commas only", col.Type)
		}
	}

	var colDefs, seqDDL []string
	for _, col := range cols {
		def := session.QuoteIdent(col.Name) + " " + col.Type
		if col.PK && engine.IsIntegerType(col.Type) {
			// The sequence lives next to its table, in the same catalog and
			// schema.
			seq := engine.AutoNumberSequence(ref.Name, col.Name)
			seqRef := session.TableRef{Catalog: ref.Catalog, Schema: ref.Schema, Name: seq}
			seqDDL = append(seqDDL, "CREATE SEQUENCE IF NOT EXISTS "+seqRef.SQL()+" START 1")
			if bareDefault {
				def += " DEFAULT nextval(" + session.QuoteLiteral(seq) + ")"
			} else {
				def += " DEFAULT nextval(" + session.QuoteLiteral(seqRef.SQL()) + ")"
			}
		}
		if col.PK {
			def += " PRIMARY KEY"
		}
		if col.NotNull {
			def += " NOT NULL"
		}
		colDefs = append(colDefs, def)
	}

	for _, ddl := range seqDDL {
		if _, err := db.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("failed to create id sequence: %w", err)
		}
	}
	if _, err := db.ExecContext(ctx, "CREATE TABLE "+ref.SQL()+" ("+strings.Join(colDefs, ", ")+")"); err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}
	return nil
}

// DropTable drops ref if it exists.
func DropTable(ctx context.Context, db *sql.DB, ref session.TableRef) error {
	if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS "+ref.SQL()); err != nil {
		return fmt.Errorf("failed to drop table: %w", err)
	}
	return nil
}

// ValidColumnType allow-lists what a DDL column-type string may contain before
// it is concatenated into CREATE TABLE / ALTER TABLE DDL: letters, digits,
// spaces, parens and commas. That is enough for "VARCHAR(64)", "DECIMAL(10,2)"
// or a type plus constraint like "INTEGER PRIMARY KEY", and rejects quotes,
// semicolons and comment sequences. DuckDB still validates the type
// semantically at Exec time. Parentheses must balance, so the text can never
// close the enclosing column list.
func ValidColumnType(t string) bool {
	t = strings.TrimSpace(t)
	if t == "" {
		return false
	}
	hasAlpha := false
	depth := 0
	for _, c := range t {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
			hasAlpha = true
		case c >= '0' && c <= '9':
		case c == '(':
			depth++
		case c == ')':
			// A ")" with no matching "(" would close the column list early and let the rest of
			// the type text become a new clause of the statement (e.g. ") AS SELECT ...").
			depth--
			if depth < 0 {
				return false
			}
		case c == ' ' || c == ',':
		default:
			return false
		}
	}
	return hasAlpha && depth == 0
}
