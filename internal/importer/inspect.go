package importer

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/session"
)

// Sentinels for Options.Key: KeyAuto adds a generated auto-number id column;
// KeyNone creates the table with no primary key. Any other value names a
// column in the file.
const (
	KeyAuto = "+auto"
	KeyNone = ""
)

// previewRowCount bounds Inspection.Rows for the wizard's preview grid.
const previewRowCount = 50

// Column describes one column of an inspected file.
type Column struct {
	Name       string  `json:"name"`        // normalized identifier (the target name)
	SourceName string  `json:"source_name"` // header as written in the file
	Inferred   ColType `json:"inferred"`
	Candidate  bool    `json:"candidate"` // unique + non-null over the full file, integer/text only
}

// Inspection is the result of Inspect: a full-file scan producing the
// preview grid, inferred types, and primary-key candidates the wizard shows
// before anything is imported.
type Inspection struct {
	Sheets       []string    `json:"sheets,omitempty"`
	Sheet        string      `json:"sheet,omitempty"`
	Columns      []Column    `json:"columns"`
	Rows         [][]*string `json:"rows"` // first 50 rows, rendered as text; NULL -> nil (a *string, not "")
	RowCount     int         `json:"row_count"`
	SuggestedKey string      `json:"suggested_key"`
}

// Inspect stages the whole file and returns its columns (with inferred
// types and key candidates), a preview of the first 50 rows, and the total
// row count.
func Inspect(ctx context.Context, db *sql.DB, s Source) (*Inspection, error) {
	if db == nil {
		return nil, fmt.Errorf("no database available")
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()
	defer dropStage(ctx, conn)

	staged, err := stage(ctx, conn, s, nil)
	if err != nil {
		return nil, err
	}

	insp := &Inspection{}
	if kind, _ := s.kind(); kind == "xlsx" {
		sheets, err := ListXLSXSheets(s.Path)
		if err != nil {
			return nil, fmt.Errorf("failed to list sheets: %w", err)
		}
		insp.Sheets = sheets
		insp.Sheet = s.Sheet
	}

	if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+stageTable).Scan(&insp.RowCount); err != nil {
		return nil, fmt.Errorf("failed to count rows: %w", err)
	}

	names := make([]string, 0, len(staged))
	used := map[string]bool{}
	cols := make([]Column, 0, len(staged))
	for _, sc := range staged {
		target := dedupeName(NormalizeName(sc.SourceName), used)
		names = append(names, target)
		cols = append(cols, Column{
			Name:       target,
			SourceName: sc.SourceName,
			Inferred:   inferType(ctx, conn, sc),
		})
	}

	candidates, err := keyCandidates(ctx, conn, staged, cols, insp.RowCount)
	if err != nil {
		return nil, err
	}
	suggested := KeyAuto
	for i := range cols {
		if candidates[names[i]] {
			cols[i].Candidate = true
			if suggested == KeyAuto && looksLikeKeyName(cols[i].Name, cols[i].SourceName) {
				suggested = cols[i].Name
			}
		}
	}
	insp.Columns = cols
	insp.SuggestedKey = suggested

	rows, err := previewRows(ctx, conn, staged, names)
	if err != nil {
		return nil, err
	}
	insp.Rows = rows

	return insp, nil
}

// inferType infers the friendly ColType for a staged column by examining its
// values over the whole file:
//   - integer family (as sniffed/read) -> integer
//   - a Promote column (forced to VARCHAR by stage() — see its doc comment)
//     is re-derived from a lossless round-trip check, THE SAME CHECK
//     castExpr's fits uses, so inference and the pre-flight fits scan can
//     never disagree (a column inference picks always passes its own
//     fits check): a sniffed DOUBLE/FLOAT/REAL column tries
//     integer/decimal(scale)/number in that order (inferNumeric); a
//     sniffed DATE/TIMESTAMP column tries date/timestamp (ISO only —
//     inferDateOrTimestamp); a column where the sniffer itself couldn't
//     settle on one type (every column promoted, sniffed recorded as
//     VARCHAR) tries the full cascade, numeric then date/timestamp then
//     boolean.
//   - DECIMAL(p,s) -> decimal s (xlsx has no DECIMAL type today, but a
//     future DuckDB reader might)
//   - VARCHAR -> text (a genuine sniffer decision, not a forced/Promote
//     column — e.g. a zip code with a leading zero must never be silently
//     promoted to Integer); DATE -> date; TIMESTAMP*/TIMESTAMPTZ ->
//     timestamp (xlsx native, no ambiguity to check); BOOLEAN -> boolean;
//     anything else (TIME, ...) -> text
func inferType(ctx context.Context, conn *sql.Conn, sc stagedCol) ColType {
	sniffed := strings.ToUpper(strings.TrimSpace(sc.SniffedType))
	col := session.QuoteIdent(sc.SourceName)

	switch {
	case engine.IsIntegerType(sniffed):
		return ColType{Type: TypeInteger}
	case sniffed == "DOUBLE" || sniffed == "FLOAT" || sniffed == "REAL":
		// Both CSV (Promote, VARCHAR-forced original text) and xlsx (native
		// DOUBLE, no separate text form — read_xlsx has no integer type at
		// all) go through the same fits-based cascade; columnFits builds
		// its check against sc.StagedType either way.
		return inferNumeric(ctx, conn, col, sc.StagedType)
	case sc.Promote && (sniffed == "DATE" || strings.HasPrefix(sniffed, "TIMESTAMP")):
		if ct, ok := inferDateOrTimestamp(ctx, conn, col, false); ok {
			return ct
		}
		return ColType{Type: TypeText}
	case sc.Promote && sniffed == "VARCHAR":
		// The sniffer couldn't settle on one type/format at all (stage()'s
		// all-varchar fallback); try every candidate, in the same priority
		// order the closed type list is offered in.
		return promoteFromText(ctx, conn, col)
	case sniffed == "DATE":
		return ColType{Type: TypeDate}
	case strings.HasPrefix(sniffed, "TIMESTAMP"):
		return ColType{Type: TypeTimestamp}
	case sniffed == "BOOLEAN":
		return ColType{Type: TypeBoolean}
	default:
		if ct, ok := FromDuckDB(sniffed); ok {
			return ct
		}
		return ColType{Type: TypeText}
	}
}

// allFit reports whether every (non-NULL, per fits' own NULL-passthrough)
// value of the staged column satisfies fits — a SQL boolean expression as
// returned by castExpr/castExprRaw. This is the full-file version of the
// same check the pre-flight fits scan runs per row, so a type inference
// picks here always passes fitsScan later; they're driven by literally the
// same predicate.
func allFit(ctx context.Context, conn *sql.Conn, fits string) bool {
	var ok sql.NullBool
	if err := conn.QueryRowContext(ctx, fmt.Sprintf("SELECT bool_and(%s) FROM %s", fits, stageTable)).Scan(&ok); err != nil {
		return false
	}
	return ok.Valid && ok.Bool
}

// columnFits is allFit for a single candidate friendly type, built from
// castExpr against the column's current (staged) DuckDB type.
func columnFits(ctx context.Context, conn *sql.Conn, col, srcType string, ct ColType) bool {
	_, fits := castExpr(col, srcType, ct)
	return allFit(ctx, conn, fits)
}

// inferNumeric decides Integer / Decimal(scale) / Number for a
// numeric-family column (VARCHAR-staged original text for CSV, native
// DOUBLE for xlsx), using columnFits at every step so inference can never
// pick a type its own value later fails to fit:
//   - a value anywhere in the column written in scientific notation forces
//     Number outright (an exponent in an otherwise-integer
//     column must not silently become an Integer with the exponent
//     evaluated away).
//   - otherwise Integer if every value round-trips through BIGINT exactly
//     (this alone rejects a 20-digit id: BIGINT overflows, so Integer
//     doesn't fit and the search continues to Decimal/Number instead of
//     silently truncating it).
//   - otherwise the smallest scale in [2,4] whose regex/overflow check
//     (castExpr's own Decimal fits, run over the WHOLE file) passes for
//     every value.
//   - otherwise Number if every value at least parses as a double.
//   - otherwise Text (some value isn't numeric at all — only reachable
//     when this column is part of stage()'s whole-file all-VARCHAR
//     fallback, not a genuine DOUBLE/FLOAT sniff).
func inferNumeric(ctx context.Context, conn *sql.Conn, col, srcType string) ColType {
	hasExp := !allFit(ctx, conn, fmt.Sprintf("(%s IS NULL OR NOT (CAST(%s AS VARCHAR) ILIKE '%%e%%'))", col, col))
	if !hasExp {
		if columnFits(ctx, conn, col, srcType, ColType{Type: TypeInteger}) {
			return ColType{Type: TypeInteger}
		}
		for s := 2; s <= 4; s++ {
			if columnFits(ctx, conn, col, srcType, ColType{Type: TypeDecimal, Scale: s}) {
				return ColType{Type: TypeDecimal, Scale: s}
			}
		}
	}
	if columnFits(ctx, conn, col, srcType, ColType{Type: TypeNumber}) {
		return ColType{Type: TypeNumber}
	}
	return ColType{Type: TypeText}
}

// inferDateOrTimestamp decides Date vs Timestamp for a VARCHAR-staged
// column (always text — a Date/Timestamp column is only ever inferred by
// re-checking original text, never a native DuckDB DATE/TIMESTAMP, which
// takes the direct sniffed-type branch in inferType instead). TRY_CAST AS
// DATE/TIMESTAMP without a session date_style only ever accepts ISO-8601,
// so a non-ISO value (any locale-ambiguous "01/02/2025") correctly fails
// every candidate here rather than being silently reinterpreted — this is
// so day/month order is never guessed. allowBoolean additionally
// tries Boolean, for stage()'s whole-file text fallback where the sniffer
// couldn't type the column at all.
func inferDateOrTimestamp(ctx context.Context, conn *sql.Conn, col string, allowBoolean bool) (ColType, bool) {
	if columnFits(ctx, conn, col, "VARCHAR", ColType{Type: TypeDate}) {
		return ColType{Type: TypeDate}, true
	}
	if columnFits(ctx, conn, col, "VARCHAR", ColType{Type: TypeTimestamp}) {
		return ColType{Type: TypeTimestamp}, true
	}
	if allowBoolean && columnFits(ctx, conn, col, "VARCHAR", ColType{Type: TypeBoolean}) {
		return ColType{Type: TypeBoolean}, true
	}
	return ColType{}, false
}

// promoteFromText is the full candidate cascade (numeric, then date/
// timestamp, then boolean, else text) for stage()'s whole-file all-VARCHAR
// fallback, where the sniffer couldn't settle on any type/format for the
// file at all and every column arrives as plain, unclassified text.
//
// This is deliberately NOT applied to a column the sniffer itself decided
// was VARCHAR (a genuine text column, e.g. a zip code with a leading
// zero) — only to Promote columns, where "text" is a fallback of last
// resort, not the sniffer's own considered decision. Silently promoting a
// zip code to Integer because "02134" round-trips through BIGINT
// numerically would lose its leading zero.
func promoteFromText(ctx context.Context, conn *sql.Conn, col string) ColType {
	if ct := inferNumeric(ctx, conn, col, "VARCHAR"); ct.Type != TypeText {
		return ct
	}
	if ct, ok := inferDateOrTimestamp(ctx, conn, col, true); ok {
		return ct
	}
	return ColType{Type: TypeText}
}

// keyCandidates finds, in one aggregate pass per integer/text column, which
// columns are unique and non-null over the whole file (rowCount > 0
// required, so an empty file suggests no key). Eligibility is based on the
// column's INFERRED friendly type, not its raw staged/sniffed DuckDB type —
// an xlsx id column is staged as native DOUBLE (read_xlsx has no integer
// type) but infers to Integer, and must still be a key candidate.
func keyCandidates(ctx context.Context, conn *sql.Conn, staged []stagedCol, cols []Column, rowCount int) (map[string]bool, error) {
	out := map[string]bool{}
	if rowCount == 0 {
		return out, nil
	}
	var parts []string
	var eligible []int
	for i, sc := range staged {
		if cols[i].Inferred.Type != TypeInteger && cols[i].Inferred.Type != TypeText {
			continue
		}
		col := session.QuoteIdent(sc.SourceName)
		parts = append(parts, fmt.Sprintf("COUNT(%s) = %d AND COUNT(DISTINCT %s) = %d", col, rowCount, col, rowCount))
		eligible = append(eligible, i)
	}
	if len(parts) == 0 {
		return out, nil
	}
	query := "SELECT " + strings.Join(parts, ", ") + " FROM " + stageTable
	dest := make([]any, len(parts))
	ptrs := make([]any, len(parts))
	for i := range dest {
		ptrs[i] = &dest[i]
	}
	if err := conn.QueryRowContext(ctx, query).Scan(ptrs...); err != nil {
		return nil, fmt.Errorf("failed to find key candidates: %w", err)
	}
	for j, idx := range eligible {
		if b, ok := dest[j].(bool); ok && b {
			out[cols[idx].Name] = true
		}
	}
	return out, nil
}

var trailingIDRe = regexp.MustCompile(`(?i)(^|_)id$`)
var sourceIDRe = regexp.MustCompile(`[a-z](Id|ID)$`)

// looksLikeKeyName reports whether a candidate column's normalized name is
// "id" or ends in "_id", or its original source header ends in "Id" or
// "ID" right after a lowercase letter (customerId, CustomerID — both
// conventions are common in exported data). "paid" must not match — the
// regexes require a word boundary (an underscore or the start of the
// string) before "id", or a case transition for the source-header form
// (both letters of "Id"/"ID" after a lowercase letter must include at
// least one uppercase, which "paid"'s all-lowercase "aid" never has).
func looksLikeKeyName(normalized, source string) bool {
	if trailingIDRe.MatchString(normalized) {
		return true
	}
	return sourceIDRe.MatchString(source)
}

// previewRows renders the first previewRowCount rows as text
// (CAST(... AS VARCHAR)), so the grid shows values exactly as DuckDB would
// print them (dates as YYYY-MM-DD, decimals at their scale) without needing
// column-type-aware formatting on the frontend. NULL renders as a nil
// string, not the text "NULL".
func previewRows(ctx context.Context, conn *sql.Conn, staged []stagedCol, names []string) ([][]*string, error) {
	exprs := make([]string, len(staged))
	for i, sc := range staged {
		exprs[i] = fmt.Sprintf("CAST(%s AS VARCHAR)", session.QuoteIdent(sc.SourceName))
	}
	query := fmt.Sprintf("SELECT %s FROM %s ORDER BY __row LIMIT %d", strings.Join(exprs, ", "), stageTable, previewRowCount)
	rows, err := conn.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to read preview rows: %w", err)
	}
	defer rows.Close()

	var out [][]*string
	for rows.Next() {
		dest := make([]sql.NullString, len(names))
		ptrs := make([]any, len(names))
		for i := range dest {
			ptrs[i] = &dest[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("failed to scan preview row: %w", err)
		}
		row := make([]*string, len(names))
		for i, d := range dest {
			if d.Valid {
				v := d.String
				row[i] = &v
			}
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

var identRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
var nonIdentCharRe = regexp.MustCompile(`[^a-zA-Z0-9_]+`)

// NormalizeName maps an arbitrary file header onto a valid SQL identifier:
// "Order Date" -> "order_date". A header that is already a valid identifier
// is kept as-is (not forced to lowercase) so a file already using clean
// snake_case names round-trips unchanged. An empty result falls back to
// "column". Caller dedupes with dedupeName.
func NormalizeName(s string) string {
	s = strings.TrimSpace(s)
	if identRe.MatchString(s) {
		return s
	}
	lower := strings.ToLower(s)
	norm := nonIdentCharRe.ReplaceAllString(lower, "_")
	norm = strings.Trim(norm, "_")
	if norm == "" {
		return "column"
	}
	if norm[0] >= '0' && norm[0] <= '9' {
		norm = "_" + norm
	}
	return norm
}

// dedupeName appends _2, _3, ... to name until it is not in used, then
// records the result. "column" duplicates (from repeated empty headers) get
// distinguished the same way, matching NormalizeName's "column" fallback.
func dedupeName(name string, used map[string]bool) string {
	if name == "column" {
		// Numbered from the start, so repeated empty headers become
		// column_1, column_2, ... rather than column, column_2, column_3.
		for i := 1; ; i++ {
			candidate := fmt.Sprintf("column_%d", i)
			if !used[candidate] {
				used[candidate] = true
				return candidate
			}
		}
	}
	if !used[name] {
		used[name] = true
		return name
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s_%d", name, i)
		if !used[candidate] {
			used[candidate] = true
			return candidate
		}
	}
}
