package semantics

// reservedKeywords are DuckDB's reserved words (keyword_category = 'reserved'
// in duckdb_keywords()): they cannot be used as a bare identifier, and a few
// of them (null, true, false) silently turn `GROUP BY name` into a constant.
// A name, column, dimension or table part that equals one is refused so that
// nothing emitted into SQL ever needs quoting. keywords_test.go asserts this
// list against a live DuckDB, so a DuckDB upgrade that adds one fails there.
var reservedKeywords = setOf(
	"all", "analyse", "analyze", "and", "any", "array", "as", "asc", "asymmetric", "both", "case", "cast", "check",
	"collate", "column", "constraint", "create", "default", "deferrable", "desc", "describe", "distinct", "do",
	"else", "end", "except", "false", "fetch", "for", "foreign", "from", "group", "having", "in", "initially",
	"intersect", "into", "lambda", "lateral", "leading", "limit", "not", "null", "offset", "on", "only", "or",
	"order", "pivot", "pivot_longer", "pivot_wider", "placing", "primary", "qualify", "references", "returning",
	"select", "show", "some", "summarize", "symmetric", "table", "then", "to", "trailing", "true", "union",
	"unique", "unpivot", "using", "variadic", "when", "where", "window", "with",
)

func setOf(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, i := range items {
		m[i] = true
	}
	return m
}
