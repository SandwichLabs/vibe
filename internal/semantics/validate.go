package semantics

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/SandwichLabs/slaw/internal/connname"
	"github.com/SandwichLabs/slaw/internal/filestore"
	"github.com/SandwichLabs/slaw/internal/queries"
	"github.com/SandwichLabs/slaw/internal/sqllex"
	"github.com/SandwichLabs/slaw/internal/strictyaml"
)

// tablePartRE is one dotted part of an entity's table: an identifier DuckDB
// takes bare, case kept.
var tablePartRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)

// AssertionKinds, Cardinalities, Formats and Verdicts are the closed value
// sets, for callers and docs. They are the only values Validate accepts.
var (
	EntityAssertionKinds = []string{AssertUnique, AssertNotNull, AssertAcceptedValues, AssertRange, AssertFreshness, AssertRowCount}
	MetricAssertionKinds = []string{AssertNonNegative, AssertNotNull, AssertRange, AssertUnique}
	Cardinalities        = []string{OneToOne, ManyToOne, OneToMany, ManyToMany}
	Formats              = []string{FormatNumber, FormatCurrency, FormatPercent}
	Verdicts             = []string{VerdictConsistent, VerdictFanOut, VerdictOrphans}
)

func oneOf(v string, set []string) bool {
	for _, s := range set {
		if v == s {
			return true
		}
	}
	return false
}

// validIdent is the rule for everything emitted into SQL as a bare identifier
// or alias: names, columns, dimensions. Lower case only (DuckDB identifiers
// are case-insensitive), so nothing emitted is ever quoted or person-shaped.
func validIdent(kind, v string) error {
	if !connname.Valid(v) {
		return invalid("%s %q must start with a lower-case letter or underscore and use only lower-case letters, digits and underscores (at most 63 characters)", kind, v)
	}
	if reservedKeywords[v] {
		return invalid("%s %q is a reserved SQL keyword; pick another name", kind, v)
	}
	return nil
}

// validName is validIdent for a definition's own name, which is also a file
// name: a Windows device name (con, nul, com1, ...) would not be a usable file
// on every machine a workspace is cloned to.
func validName(kind, v string) error {
	if err := validIdent(kind, v); err != nil {
		return err
	}
	if !strings.HasPrefix(v, "_") && !filestore.ValidID(v) {
		return invalid("%s %q is a reserved file name; pick another", kind, v)
	}
	return nil
}

func checkText(field, v string, max int, required bool) error {
	if required && strings.TrimSpace(v) == "" {
		return invalid("%s is required", field)
	}
	if n := utf8.RuneCountInString(v); n > max {
		return invalid("%s is %d characters, over the %d character limit", field, n, max)
	}
	return noHidden(field, v, false)
}

// noHidden refuses a decoded value that holds a control or bidi character. The
// byte scan in strictyaml sees the file as written; a YAML escape such as
// "\u0007" only becomes a control character once decoded.
func noHidden(field, v string, invisible bool) error {
	if r, ok := strictyaml.FirstHiddenRune(v, invisible); ok {
		return invalid("%s contains a control or invisible character (U+%04X)", field, r)
	}
	return nil
}

func checkVersion(v int) error {
	if v != Version {
		return invalid("version must be %d (got %d)", Version, v)
	}
	return nil
}

// ValidateEntity checks an entity's structure.
func ValidateEntity(e *Entity) error {
	if err := checkVersion(e.Version); err != nil {
		return err
	}
	if err := validName("entity name", e.Name); err != nil {
		return err
	}
	if err := checkText("description", e.Description, MaxDescription, false); err != nil {
		return err
	}
	if err := checkText("grain", e.Grain, MaxGrain, true); err != nil {
		return err
	}
	if err := checkTable(e.Table); err != nil {
		return err
	}
	if e.Connection != "" {
		if err := connname.Validate(e.Connection); err != nil {
			return invalid("connection: %v", err)
		}
	}
	if e.IDColumn == "" {
		return invalid("id_column is required")
	}
	if err := validIdent("id_column", e.IDColumn); err != nil {
		return err
	}
	if err := checkText("owner", e.Owner, MaxOwner, false); err != nil {
		return err
	}
	if err := checkColumns(e.Columns); err != nil {
		return err
	}
	return checkAssertions(e.Assertions, EntityAssertionKinds, true, 0)
}

func checkTable(table string) error {
	if table == "" {
		return invalid("table is required")
	}
	parts := strings.Split(table, ".")
	if len(parts) > 3 {
		return invalid("table %q has %d parts; use table, schema.table or catalog.schema.table", table, len(parts))
	}
	for _, p := range parts {
		if !tablePartRE.MatchString(p) {
			return invalid("table %q: %q must start with a letter or underscore and use only letters, digits and underscores (at most 63 characters)", table, p)
		}
		if reservedKeywords[strings.ToLower(p)] {
			return invalid("table %q: %q is a reserved SQL keyword; pick another name", table, p)
		}
	}
	return nil
}

func checkColumns(cols []Column) error {
	if len(cols) > MaxColumns {
		return invalid("%d columns, over the limit of %d", len(cols), MaxColumns)
	}
	seen := map[string]bool{}
	for _, c := range cols {
		if err := validIdent("column name", c.Name); err != nil {
			return err
		}
		if seen[c.Name] {
			return invalid("column %q is listed twice", c.Name)
		}
		seen[c.Name] = true
		if err := checkText("column "+c.Name+" description", c.Description, MaxDescription, false); err != nil {
			return err
		}
	}
	return nil
}

// maxAgeRE is a freshness window: whole days or hours, 1 to 99999.
var maxAgeRE = regexp.MustCompile(`^[1-9][0-9]{0,4}[dh]$`)

// MaxBound is the largest magnitude a range bound may have.
const MaxBound = 1e15

// MaxMinRows is the largest min_rows.
const MaxMinRows = 1_000_000_000_000_000

// checkAssertions validates a list against the kinds one owner takes. Entity
// assertions name a column (except row_count); accepted_values also lists the
// values, range its bounds, freshness its max_age and row_count its min_rows.
// dims is a metric's dimension count (a metric's unique needs at least one).
func checkAssertions(list []Assertion, kinds []string, entity bool, dims int) error {
	if len(list) > MaxAssertions {
		return invalid("%d assertions, over the limit of %d", len(list), MaxAssertions)
	}
	for i, a := range list {
		if !oneOf(a.Kind, kinds) {
			return invalid("assertion %d: kind %q is not one of %s", i+1, a.Kind, strings.Join(kinds, ", "))
		}
		label := fmt.Sprintf("assertion %d (%s)", i+1, a.Kind)
		if err := checkAssertionFields(label, a, entity, dims); err != nil {
			return err
		}
	}
	return nil
}

// checkAssertionFields holds one assertion to the fields its kind takes and
// refuses every field that belongs to another kind.
func checkAssertionFields(label string, a Assertion, entity bool, dims int) error {
	hasBounds := a.Min != nil || a.Max != nil
	if a.Kind != AssertRange && hasBounds {
		return invalid("%s: min and max belong to range only", label)
	}
	if a.Kind != AssertFreshness && a.MaxAge != "" {
		return invalid("%s: max_age belongs to freshness only", label)
	}
	if a.Kind != AssertRowCount && a.MinRows != nil {
		return invalid("%s: min_rows belongs to row_count only", label)
	}
	if a.Kind != AssertAcceptedValues && len(a.Values) > 0 {
		if entity {
			return invalid("%s: values belong to accepted_values only", label)
		}
		return invalid("%s: a metric assertion takes no column or values", label)
	}
	if !entity {
		return checkMetricAssertion(label, a, dims)
	}
	if a.Kind == AssertRowCount {
		if a.Column != "" {
			return invalid("%s: row_count counts the table's rows and takes no column", label)
		}
	} else if err := validIdent("assertion "+a.Kind+" column", a.Column); err != nil {
		return err
	}
	switch a.Kind {
	case AssertAcceptedValues:
		return checkAcceptedValues(label, a.Values)
	case AssertRange:
		return checkRange(label, a)
	case AssertFreshness:
		return checkFreshness(label, a)
	case AssertRowCount:
		return checkRowCount(label, a)
	}
	return nil
}

func checkMetricAssertion(label string, a Assertion, dims int) error {
	if a.Column != "" {
		return invalid("%s: a metric assertion takes no column or values", label)
	}
	switch a.Kind {
	case AssertRange:
		return checkRange(label, a)
	case AssertUnique:
		if dims == 0 {
			return invalid("%s: unique on a metric checks that its saved query has one row per dimension set, so the metric needs at least one dimension", label)
		}
	}
	return nil
}

func checkAcceptedValues(label string, values []string) error {
	if len(values) == 0 || len(values) > MaxValues {
		return invalid("%s: needs 1 to %d values (got %d)", label, MaxValues, len(values))
	}
	for _, v := range values {
		if utf8.RuneCountInString(v) > MaxValueLen {
			return invalid("%s: a value is over %d characters", label, MaxValueLen)
		}
		if err := noHidden("an accepted value", v, false); err != nil {
			return err
		}
	}
	return nil
}

func checkRange(label string, a Assertion) error {
	if a.Min == nil && a.Max == nil {
		return invalid("%s: needs min, max or both", label)
	}
	for _, b := range []*Number{a.Min, a.Max} {
		if b == nil {
			continue
		}
		f := b.Float()
		// The bound must survive a round trip through the file's plain decimal form.
		if math.IsNaN(f) || math.Abs(f) >= MaxBound || !numberRE.MatchString(strconv.FormatFloat(f, 'f', -1, 64)) {
			return invalid("%s: a bound must be a plain decimal number under %g in size with at most 15 decimals", label, MaxBound)
		}
	}
	if a.Min != nil && a.Max != nil && *a.Min > *a.Max {
		return invalid("%s: min %v is above max %v", label, float64(*a.Min), float64(*a.Max))
	}
	return nil
}

func checkFreshness(label string, a Assertion) error {
	if !maxAgeRE.MatchString(a.MaxAge) {
		return invalid("%s: max_age must be a whole number of days or hours such as 7d or 36h (got %q)", label, a.MaxAge)
	}
	return nil
}

func checkRowCount(label string, a Assertion) error {
	if a.MinRows == nil || *a.MinRows < 1 || *a.MinRows > MaxMinRows {
		return invalid("%s: min_rows is required and must be a whole number from 1 to %d", label, int64(MaxMinRows))
	}
	return nil
}

// MaxAgeDuration reads a freshness max_age ("7d", "36h") as a duration.
func MaxAgeDuration(s string) (time.Duration, error) {
	if !maxAgeRE.MatchString(s) {
		return 0, invalid("max_age must be a whole number of days or hours such as 7d or 36h (got %q)", s)
	}
	n, err := strconv.Atoi(s[:len(s)-1])
	if err != nil {
		return 0, invalid("max_age %q: %v", s, err)
	}
	unit := time.Hour
	if s[len(s)-1] == 'd' {
		unit = 24 * time.Hour
	}
	return time.Duration(n) * unit, nil
}

// ValidateMetric checks a metric's structure and the lexical shape of its
// measure. What the measure means in SQL is vetted where it runs.
func ValidateMetric(m *Metric) error {
	if err := checkVersion(m.Version); err != nil {
		return err
	}
	if err := validName("metric name", m.Name); err != nil {
		return err
	}
	if err := checkText("title", m.Title, MaxTitle, false); err != nil {
		return err
	}
	if err := checkText("description", m.Description, MaxDescription, false); err != nil {
		return err
	}
	if m.Query == "" {
		return invalid("query is required: the id of a saved query (queries/<id>.sql)")
	}
	if !filestore.ValidID(m.Query) {
		return invalid("query %q is not a saved query id: use lowercase letters, digits, - and _", m.Query)
	}
	if err := checkMeasure(m.Measure); err != nil {
		return err
	}
	if err := checkDimensions(m.Dimensions); err != nil {
		return err
	}
	if m.Format != "" && !oneOf(m.Format, Formats) {
		return invalid("format %q is not one of %s", m.Format, strings.Join(Formats, ", "))
	}
	if err := checkText("owner", m.Owner, MaxOwner, false); err != nil {
		return err
	}
	if m.SettledBy != nil {
		if err := checkText("settled_by.text", m.SettledBy.Text, MaxSettledText, true); err != nil {
			return err
		}
		if err := checkText("settled_by.ref", m.SettledBy.Ref, MaxRef, false); err != nil {
			return err
		}
	}
	return checkAssertions(m.Assertions, MetricAssertionKinds, false, len(m.Dimensions))
}

func checkDimensions(dims []string) error {
	if len(dims) > MaxDimensions {
		return invalid("%d dimensions, over the limit of %d", len(dims), MaxDimensions)
	}
	seen := map[string]bool{}
	for _, d := range dims {
		if err := validIdent("dimension", d); err != nil {
			return err
		}
		if seen[d] {
			return invalid("dimension %q is listed twice", d)
		}
		seen[d] = true
	}
	return nil
}

// measureBannedWords may not appear in a measure at all: each starts a new
// clause, a second query or a statement-shaped subquery (`(PIVOT ...)`,
// `(TABLE t)`, `(DESCRIBE ...)`, `EXISTS (SHOW ...)`), which would break out of
// `<measure> AS name` or read something the saved query never named.
var measureBannedWords = setOf("select", "from", "with", "union", "intersect", "except", "as", "group", "having",
	"order", "limit", "offset", "qualify", "window",
	"pivot", "pivot_wider", "pivot_longer", "unpivot", "table", "values", "describe", "summarize", "show",
	"explain", "pragma", "call", "exists")

// measureDeniedFunctions are functions that are in duckdb_functions() as
// scalar or aggregate but that a measure may not call: environment and
// session state, the query() family, side effects, more than one column or
// row, remote services and secrets. The allow-list (allowedFunctions) is
// generated from DuckDB minus these, so a new DuckDB function is refused until
// the list is regenerated (functions_test.go).
var measureDeniedFunctions = func() map[string]bool {
	m := setOf("getenv", "query", "query_table", "metric",
		"columns", "unnest",
		"nextval", "setval", "currval", "current_setting", "getvariable", "setvariable",
		"which_secret", "prompt", "embedding", "get_md_token", "print_md_token",
		"sleep_ms", "write_log", "setseed")
	for _, f := range queries.ReaderFunctions {
		m[f] = true
	}
	return m
}()

// syntaxFunctions are callable names that DuckDB parses as keyword syntax, so
// duckdb_functions() does not list them.
var syntaxFunctions = setOf("coalesce", "nullif")

// callable reports a name a measure may call.
func callable(name string) bool { return allowedFunctions[name] || syntaxFunctions[name] }

// afterParenOK are the keywords that may open a parenthesised group in a
// measure. Any other keyword (of any DuckDB category) directly after "(" is
// refused, so a statement-shaped subquery cannot start there; a keyword that
// is a callable function (coalesce, left, ...) is let through when it is itself
// followed by "(" and is on the function allow-list.
var afterParenOK = setOf("distinct", "case", "not", "null", "true", "false", "interval")

// beforeParenOK are keywords that may stand directly before "(" without being
// a function call: IN (...), FILTER (...), AND (...), and the like.
var beforeParenOK = setOf("where", "in", "not", "and", "or", "filter", "over", "when", "then", "else", "case", "between",
	"is", "distinct", "like", "ilike", "any", "all", "interval", "end")

// checkMeasure holds the measure to what can be emitted on one SQL line as one
// aggregate expression after `SELECT ` and before ` AS name`:
//
//   - non-empty, at most 4 KiB, one line, no `;`, no `--` or `/*`;
//   - balanced parentheses; only plain 'strings' (no E'..' or $$..$$), every
//     quote closed;
//   - no top-level comma (commas inside a call's parentheses are fine);
//   - none of SELECT, FROM, WITH, UNION, INTERSECT, EXCEPT, AS, GROUP, HAVING,
//     ORDER, LIMIT, OFFSET, QUALIFY, WINDOW anywhere, and WHERE only directly
//     inside FILTER (...);
//   - every call is to a function on the generated allow-list (DuckDB's scalar
//     and aggregate functions less measureDeniedFunctions: getenv, query,
//     the file readers, COLUMNS, UNNEST, nextval and the other sequence and
//     setting functions, metric, secrets and remote services), unqualified;
//     FILTER, OVER, IN and the like before "(" are not calls; a type after
//     "::" may take arguments (::DECIMAL(10,2));
//   - after "(" the next word may not be any DuckDB keyword (so no
//     `(SELECT ...)`, `(PIVOT ...)`, `(TABLE t)`, `(DESCRIBE ...)`, `(VALUES ...)`)
//     except DISTINCT, CASE, NOT, NULL, TRUE, FALSE, INTERVAL, WHERE after
//     FILTER, and a callable keyword such as coalesce( that is itself on the
//     allow-list;
//   - no `*` as a star (`t.*`, `* EXCLUDE (...)`, `sum(*)`) except in
//     count(*); a `*` between two operands is multiplication and is fine.
//
// It does not decide that the expression is an aggregate; DuckDB and the SQL
// policy do that where it runs.
func checkMeasure(measure string) error {
	if strings.TrimSpace(measure) == "" {
		return invalid("measure is required: one SQL aggregate expression such as sum(amount)")
	}
	if len(measure) > MaxMeasureBytes {
		return invalid("measure is %d bytes, over the %d byte limit", len(measure), MaxMeasureBytes)
	}
	switch {
	case strings.ContainsAny(measure, ";\n\r"):
		return invalid("measure must be one expression on one line, without a semicolon")
	case strings.Contains(measure, "--") || strings.Contains(measure, "/*"):
		return invalid("measure may not contain a comment (-- or /*)")
	}
	if err := noHidden("measure", measure, true); err != nil {
		return err
	}
	stmts := sqllex.Statements(measure)
	if len(stmts) != 1 {
		return invalid("measure must lex as one SQL expression")
	}
	return checkMeasureTokens(stmts[0])
}

func checkMeasureTokens(toks []sqllex.Tok) error {
	depth := 0
	for i, t := range toks {
		switch {
		case t.IsParen("("):
			depth++
		case t.IsParen(")"):
			if depth--; depth < 0 {
				return invalid("measure has an unbalanced parenthesis")
			}
		case t.IsPunct(",") && depth == 0:
			return invalid("measure must be one expression: a comma outside parentheses would add a second column")
		case t.Kind == sqllex.String && !t.Plain:
			if !t.Closed {
				return invalid("measure has an unterminated quote")
			}
			return invalid("measure may use only plain 'quoted' strings (no E'..' or $$..$$)")
		case t.Kind == sqllex.Ident && !t.Closed:
			return invalid("measure has an unterminated quote")
		case t.Kind == sqllex.Word && measureBannedWords[strings.ToLower(t.Text)]:
			return invalid("measure may not use %s: it must be a single expression (use FILTER (WHERE ...) or CASE WHEN to restrict rows)", t.Text)
		case t.Kind == sqllex.Word && t.Text == "WHERE" && !(i >= 2 && toks[i-1].IsParen("(") && toks[i-2].IsWord("FILTER")):
			return invalid("measure may use WHERE only inside FILTER (...)")
		}
		if err := checkMeasureParens(toks, i); err != nil {
			return err
		}
		if t.IsPunct("*") && isStar(toks, i) && !isCountStar(toks, i) {
			return invalid("measure may use * only in count(*): a metric must produce exactly one value column")
		}
	}
	if depth != 0 {
		return invalid("measure has an unbalanced parenthesis")
	}
	return nil
}

// ValidateJoin checks a join's structure.
func ValidateJoin(j *Join) error {
	if err := checkVersion(j.Version); err != nil {
		return err
	}
	if err := validName("join name", j.Name); err != nil {
		return err
	}
	if err := validIdent("left entity", j.Left); err != nil {
		return err
	}
	if err := validIdent("right entity", j.Right); err != nil {
		return err
	}
	if len(j.On) == 0 || len(j.On) > MaxJoinColumns {
		return invalid("on needs 1 to %d column pairs (got %d)", MaxJoinColumns, len(j.On))
	}
	for _, p := range j.On {
		if err := validIdent("on.left column", p.Left); err != nil {
			return err
		}
		if err := validIdent("on.right column", p.Right); err != nil {
			return err
		}
	}
	if !oneOf(j.Cardinality, Cardinalities) {
		return invalid("cardinality %q is not one of %s", j.Cardinality, strings.Join(Cardinalities, ", "))
	}
	if err := checkText("description", j.Description, MaxDescription, false); err != nil {
		return err
	}
	return checkMeasured(j.Measured)
}

func checkMeasured(m *Measured) error {
	if m == nil {
		return nil
	}
	if _, err := time.Parse(time.RFC3339, m.At); err != nil {
		return invalid("measured.at %q is not an RFC 3339 time", m.At)
	}
	counts := []struct {
		field string
		n     Count
	}{{"left_rows", m.LeftRows}, {"right_rows", m.RightRows}, {"max_fanout", m.MaxFanout},
		{"left_orphans", m.LeftOrphans}, {"right_orphans", m.RightOrphans}}
	for _, c := range counts {
		if c.n < 0 {
			return invalid("measured.%s may not be negative", c.field)
		}
	}
	if !oneOf(m.Verdict, Verdicts) {
		return invalid("measured.verdict %q is not one of %s", m.Verdict, strings.Join(Verdicts, ", "))
	}
	return nil
}

// isStar reports whether the `*` at toks[i] is a star expression rather than
// multiplication: multiplication has an operand on its left and one on its
// right. count(*), sum(*), t.* and * EXCLUDE (...) are all stars.
func isStar(toks []sqllex.Tok, i int) bool {
	if i == 0 {
		return true
	}
	p := toks[i-1]
	leftOperand := p.Kind == sqllex.Word || p.Kind == sqllex.Ident || p.Kind == sqllex.String || p.IsParen(")")
	if !leftOperand {
		return true
	}
	if i+1 >= len(toks) {
		return true
	}
	n := toks[i+1]
	return n.IsParen(")") || n.IsPunct(",") || n.IsPunct("*")
}

// isCountStar reports count(*): the star alone inside the parentheses of count.
func isCountStar(toks []sqllex.Tok, i int) bool {
	return i >= 2 && i+1 < len(toks) && toks[i-1].IsParen("(") && toks[i+1].IsParen(")") &&
		toks[i-2].IsName() && strings.EqualFold(toks[i-2].Text, "count")
}

// checkMeasureParens applies the structural rules around toks[i]: what may
// follow an opening parenthesis, and which names may be called.
func checkMeasureParens(toks []sqllex.Tok, i int) error {
	t := toks[i]
	if t.IsParen("(") && i+1 < len(toks) {
		if err := afterParen(toks, i); err != nil {
			return err
		}
	}
	if !t.IsName() || i+1 >= len(toks) || !toks[i+1].IsParen("(") {
		return nil
	}
	name := strings.ToLower(t.Text)
	switch {
	case i > 0 && toks[i-1].IsPunct("."):
		return invalid("measure may not call a qualified function (%s)", name)
	case i >= 2 && toks[i-1].IsPunct(":") && toks[i-2].IsPunct(":"):
		return nil // a type: ::DECIMAL(10,2)
	case measureDeniedFunctions[name]:
		return invalid("measure may not call %s()", name)
	case callable(name):
		return nil
	case t.Kind == sqllex.Word && beforeParenOK[name]:
		return nil
	}
	return invalid("measure may not call %s(): only DuckDB's scalar and aggregate functions are allowed", name)
}

// afterParen refuses a DuckDB keyword directly after "(" (see afterParenOK).
func afterParen(toks []sqllex.Tok, i int) error {
	n := toks[i+1]
	if n.Kind != sqllex.Word {
		return nil
	}
	w := strings.ToLower(n.Text)
	switch {
	case afterParenOK[w], !allKeywords[w]:
		return nil
	case w == "where" && i > 0 && toks[i-1].IsWord("FILTER"):
		return nil
	case i+2 < len(toks) && toks[i+2].IsParen("(") && callable(w):
		return nil // coalesce(, left(: a callable keyword
	}
	return invalid("measure may not start a group with %s: a subquery or statement cannot hide inside a measure", strings.ToUpper(w))
}
