package semantics

import (
	"strings"

	"github.com/SandwichLabs/assay/internal/session"
)

// QuoteTable quotes each dotted part of an entity's table with the one
// identifier quoter (session.QuoteIdent). The parts are already validated, so
// this is a second safeguard, and it also stops a name DuckDB reads as a
// function or keyword (a column called current_date) from meaning something else.
func QuoteTable(table string) string {
	parts := strings.Split(table, ".")
	for i, p := range parts {
		parts[i] = session.QuoteIdent(p)
	}
	return strings.Join(parts, ".")
}
