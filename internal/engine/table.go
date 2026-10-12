package engine

import (
	"fmt"

	"github.com/SandwichLabs/vibe/internal/session"
)

// validPart reports whether a catalog or schema name is a safe identifier
// (same alphabet as ValidIdentifier, without its "_vibe_" reservation, which
// protects table names only).
func validPart(name string) bool {
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

// ParseTable parses and validates a table identifier arriving from the
// frontend, the agent or a CLI flag: "t", "schema.t" or "catalog.schema.t".
// Each part must satisfy the identifier rules (the table part additionally
// ValidIdentifier's "_vibe_" reservation). It does not touch the database;
// parts left empty resolve later against the session default.
func ParseTable(name string) (session.TableRef, error) {
	ref, err := session.ParseTableRef(name)
	if err != nil {
		return session.TableRef{}, fmt.Errorf("invalid table name %q: %w", name, err)
	}
	if !ValidIdentifier(ref.Name) {
		return session.TableRef{}, fmt.Errorf("invalid table name %q", name)
	}
	if ref.Catalog != "" && !validPart(ref.Catalog) {
		return session.TableRef{}, fmt.Errorf("invalid catalog %q in table name %q", ref.Catalog, name)
	}
	if ref.Schema != "" && !validPart(ref.Schema) {
		return session.TableRef{}, fmt.Errorf("invalid schema %q in table name %q", ref.Schema, name)
	}
	return ref, nil
}

// TableSQL renders a table argument as quoted SQL: a bare name is one quoted
// identifier (as it always was), "schema.table" / "catalog.schema.table" quote
// every part. An unqualified table resolves, in DuckDB, against the
// connection's default catalog, which is DefaultCatalog() on every pooled
// connection. A name that does not parse is quoted whole, so it can never
// splice SQL. Callers that need validation use ParseTable first.
func TableSQL(name string) string {
	ref, err := session.ParseTableRef(name)
	if err != nil {
		return session.QuoteIdent(name)
	}
	return ref.SQL()
}
