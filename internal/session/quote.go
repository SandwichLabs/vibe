package session

import (
	"fmt"
	"strings"
)

// QuoteIdent quotes a DuckDB identifier (catalog, schema, table or column
// name) for interpolation into SQL. DuckDB identifiers are double-quoted SQL
// identifiers whose only escape is doubling the quote character. NUL bytes
// are dropped: they are never legal in an identifier and a C-string boundary
// in the driver could truncate the statement at one.
//
// This is the ONE identifier quoter for the codebase (session.QuoteIdent
// delegates here). All ATTACH / secret / USE SQL the session builds goes
// through it and QuoteLiteral; nothing is concatenated raw.
func QuoteIdent(ident string) string {
	ident = strings.ReplaceAll(ident, "\x00", "")
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}

// QuoteLiteral quotes a string as a DuckDB string literal ('it”s'). DuckDB
// does not treat backslash as an escape in a standard literal, so doubling the
// single quote is the complete escape. NUL bytes are dropped (see QuoteIdent).
func QuoteLiteral(s string) string {
	s = strings.ReplaceAll(s, "\x00", "")
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// QuoteQualified quotes each non-empty part and joins them with dots:
// QuoteQualified("db", "main", "t") is "db"."main"."t".
func QuoteQualified(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, QuoteIdent(p))
	}
	return strings.Join(out, ".")
}

// TableRef is a (possibly partially specified) table reference. Empty
// Catalog/Schema mean "the current default".
type TableRef struct {
	Catalog string
	Schema  string
	Name    string
}

// IsQualified reports whether the reference names a catalog explicitly.
func (r TableRef) IsQualified() bool { return r.Catalog != "" }

// SQL renders the reference with every part quoted; omitted parts are skipped.
func (r TableRef) SQL() string {
	var parts []string
	for _, p := range []string{r.Catalog, r.Schema, r.Name} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return QuoteQualified(parts...)
}

// String is the unquoted dotted form, for messages.
func (r TableRef) String() string {
	var parts []string
	for _, p := range []string{r.Catalog, r.Schema, r.Name} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, ".")
}

// ParseTableRef parses "table", "schema.table" or "catalog.schema.table".
// Parts may be double-quoted (with "" as the escape) so names containing dots
// survive: `"my.table"` is one part. Unquoted parts are used verbatim (not
// case-folded; the introspection queries compare case-insensitively where the
// legacy code did).
func ParseTableRef(s string) (TableRef, error) {
	parts, err := splitQualified(s)
	if err != nil {
		return TableRef{}, err
	}
	switch len(parts) {
	case 1:
		return TableRef{Name: parts[0]}, nil
	case 2:
		return TableRef{Schema: parts[0], Name: parts[1]}, nil
	case 3:
		return TableRef{Catalog: parts[0], Schema: parts[1], Name: parts[2]}, nil
	default:
		return TableRef{}, fmt.Errorf("invalid table reference %q: expected table, schema.table or catalog.schema.table", s)
	}
}

func splitQualified(s string) ([]string, error) {
	var parts []string
	var cur strings.Builder
	inQuote := false
	wasQuoted := false
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case inQuote && r == '"':
			if i+1 < len(rs) && rs[i+1] == '"' {
				cur.WriteRune('"')
				i++
			} else {
				inQuote = false
			}
		case inQuote:
			cur.WriteRune(r)
		case r == '"' && cur.Len() == 0 && !wasQuoted:
			inQuote, wasQuoted = true, true
		case r == '.':
			if cur.Len() == 0 && !wasQuoted {
				return nil, fmt.Errorf("invalid table reference %q: empty name part", s)
			}
			parts = append(parts, cur.String())
			cur.Reset()
			wasQuoted = false
		default:
			if wasQuoted {
				return nil, fmt.Errorf("invalid table reference %q: text after closing quote", s)
			}
			cur.WriteRune(r)
		}
	}
	if inQuote {
		return nil, fmt.Errorf("invalid table reference %q: unterminated quote", s)
	}
	if cur.Len() == 0 && !wasQuoted {
		return nil, fmt.Errorf("invalid table reference %q: empty name part", s)
	}
	return append(parts, cur.String()), nil
}

// SelectAllSQL is "SELECT * FROM <ref>" with every part of the reference quoted.
func (r TableRef) SelectAllSQL() string { return "SELECT * FROM " + r.SQL() }

// CopyToSQL wraps a query in COPY (…) TO '<dst>' WITH (<opts>). Trailing
// whitespace and semicolons are stripped from query (a query that runs fine with
// a trailing ";" must export too: DuckDB rejects a second statement inside the
// COPY's parenthesized subquery), and the closing paren goes on its own line so
// a trailing "-- comment" in query cannot swallow it. dst is quoted with
// QuoteLiteral, so a path containing a single quote stays one literal. opts is
// caller-built option text (never user input). It does not try to parse comments
// out of query; the caller must have checked that query is a single SELECT.
func CopyToSQL(query, dst, opts string) string {
	query = strings.TrimRight(query, " \t\r\n;")
	return "COPY (" + query + "\n) TO " + QuoteLiteral(dst) + " WITH (" + opts + ")"
}
