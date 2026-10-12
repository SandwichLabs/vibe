package engine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/SandwichLabs/vibe/internal/session"
)

// IsIntegerType reports whether a DuckDB type name is in the integer family
// eligible for auto-numbering (a sequence-backed primary key). Shared by
// container's auto-number backfill, Studio's CreateTable, and the importer's
// create/replace path — DuckDB has no implicit AUTOINCREMENT, so every one of
// these needs the same "is this an integer column" test.
func IsIntegerType(t string) bool {
	switch strings.ToUpper(strings.TrimSpace(t)) {
	case "BIGINT", "INTEGER", "INT", "INT2", "INT4", "INT8", "SMALLINT", "TINYINT",
		"HUGEINT", "UBIGINT", "UINTEGER", "USMALLINT", "UTINYINT":
		return true
	}
	return false
}

// AutoNumberSequence returns the deterministic sequence name backing an
// auto-number primary key column. Both identifiers must be validated by the
// caller before this is interpolated into DDL.
func AutoNumberSequence(table, column string) string {
	return fmt.Sprintf("_vibe_seq_%s_%s", table, column)
}

// MaxAutoNumberGap caps how far BumpAutoNumber will advance a sequence in
// one call. Consuming nextval() in a loop is the only portable re-seed (see
// BumpAutoNumber's doc comment), but its cost is linear in the gap —
// measured at ~22ns per value, so an external or date-coded id around 1e10
// would hang the caller (Studio's write lock, or the CLI) for minutes.
// Exported so a caller can pre-flight this BEFORE writing anything (see
// AutoNumberGap) instead of finding out only after the write already ran.
const MaxAutoNumberGap = 10_000_000

// Querier is the subset of *sql.DB / *sql.Tx / *sql.Conn that BumpAutoNumber
// needs, so it can run inside a transaction (the importer) or standalone
// (container's backfill).
type Querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// AutoNumberGap reports how far column's auto-number sequence would need to
// advance to clear maxVal, WITHOUT consuming a nextval() (peeking via
// duckdb_sequences()' last_value/start_value instead) — so a caller can
// pre-flight "would this be safe to bump" before writing anything, and a
// refusal never has the side effect of moving the sequence at all. ok is
// false when the column has no sequence-backed auto-number default at all
// (nothing to bump); gap is only meaningful when ok is true.
//
// table/column are matched case-insensitively against information_schema
// (DuckDB's own quoted-identifier catalog lookup is case-insensitive, but a
// plain information_schema string compare is not — see Import's own comment
// on canonicalizing the table name after a case-different match).
func AutoNumberGap(ctx context.Context, q Querier, ref session.TableRef, column string, maxVal int64) (gap int64, ok bool, err error) {
	table := ref.Name
	var hasDefault bool
	var def sql.NullString
	cond, args := session.RefFilter(ref, "table_catalog", "table_schema", "table_name", 1)
	err = q.QueryRowContext(ctx,
		`SELECT column_default IS NOT NULL, column_default
		   FROM information_schema.columns
		  WHERE `+cond+fmt.Sprintf(" AND lower(column_name) = lower($%d)", len(args)+1),
		append(args, column)...,
	).Scan(&hasDefault, &def)
	if err != nil {
		return 0, false, fmt.Errorf("failed to inspect column %q.%q: %w", table, column, err)
	}
	if !hasDefault || !def.Valid {
		return 0, false, nil
	}
	seq := AutoNumberSequence(table, column)
	// The default text is nextval('seq') for a table in the default catalog
	// and nextval('"catalog"."schema"."seq"') for one created qualified: take
	// the literal and compare its last name part whole (seq must not match seq2).
	if !defaultUsesSequence(def.String, seq) {
		// Default isn't our auto-number sequence (e.g. user-authored default).
		return 0, false, nil
	}

	cur, found, err := sequencePosition(ctx, q, ref, seq)
	if err != nil {
		return 0, false, err
	}
	if !found {
		return 0, false, nil
	}
	if maxVal <= cur {
		return 0, true, nil
	}
	return maxVal - cur, true, nil
}

// sequencePosition reads a sequence's last dispensed value (a
// conservative lower bound, see below) from
// duckdb_sequences() — its last_value column, or start_value - 1 when
// last_value is still NULL (the sequence has never had nextval() called on
// it in any session) — WITHOUT itself calling nextval(). found is false
// only if the sequence doesn't exist at all (it should always exist for an
// auto-number column; a caller can treat !found the same as "nothing to
// bump").
func sequencePosition(ctx context.Context, q Querier, ref session.TableRef, seq string) (pos int64, found bool, err error) {
	var lastVal, startVal, incr sql.NullInt64
	seqRef := session.TableRef{Catalog: ref.Catalog, Schema: ref.Schema, Name: seq}
	cond, args := session.RefFilter(seqRef, "database_name", "schema_name", "sequence_name", 1)
	err = q.QueryRowContext(ctx,
		`SELECT last_value, start_value, increment_by FROM duckdb_sequences() WHERE `+cond,
		args...,
	).Scan(&lastVal, &startVal, &incr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("failed to inspect sequence %q: %w", seq, err)
	}
	if lastVal.Valid {
		// DuckDB rewrites a persisted sequence on checkpoint so that, after the
		// file is reopened, start_value and last_value both hold the NEXT value
		// to dispense, not the last one dispensed (#19). In-session, last_value
		// equals start_value only right after the first nextval(), so equality
		// is ambiguous. Read it conservatively as "next": the position may then
		// be one too low (the caller bumps one extra value, a harmless gap) but
		// never one too high (a default insert colliding with an existing key).
		if startVal.Valid && lastVal.Int64 == startVal.Int64 {
			step := int64(1)
			if incr.Valid && incr.Int64 != 0 {
				step = incr.Int64
			}
			return lastVal.Int64 - step, true, nil
		}
		return lastVal.Int64, true, nil
	}
	if startVal.Valid {
		return startVal.Int64 - 1, true, nil
	}
	return 0, false, nil
}

// BumpAutoNumber re-seeds an auto-number sequence past the current max value
// of its column, if the column's DEFAULT is nextval('<seq>'). It is the only
// portable way to do this on DuckDB 1.4.1:
//
//   - ALTER COLUMN ... SET DEFAULT / DROP DEFAULT fails when the table is
//     referenced by a foreign key ("violates foreign key constraint" style
//     dependency errors from the catalog).
//   - CREATE OR REPLACE SEQUENCE fails because the table's DEFAULT expression
//     depends on the existing sequence.
//
// The only operation DuckDB allows unconditionally is consuming nextval()
// itself, so this repeatedly advances the sequence with
// `SELECT max(nextval(seq)) FROM range(n)` until it has passed the column's
// current max. Gaps in the sequence are an accepted side effect — auto-number values are surrogate keys, not a
// user-facing counter.
//
// A gap over MaxAutoNumberGap is refused (see AutoNumberGap's doc comment
// for why the check itself never consumes a nextval); callers that write
// through a transaction should prefer pre-flighting with AutoNumberGap so a
// refusal here is unreachable (a mid-transaction failure here still rolls
// the whole write back, but a caller that already knows the gap is too
// large can give a much earlier, friendlier answer — see the importer's own
// pre-flight).
func BumpAutoNumber(ctx context.Context, q Querier, ref session.TableRef, column string) error {
	table := ref.Name
	var maxVal sql.NullInt64
	if err := q.QueryRowContext(ctx, fmt.Sprintf("SELECT MAX(%s) FROM %s", session.QuoteIdent(column), ref.SQL())).Scan(&maxVal); err != nil {
		return fmt.Errorf("failed to read max %q: %w", column, err)
	}
	if !maxVal.Valid {
		return nil
	}

	gap, ok, err := AutoNumberGap(ctx, q, ref, column, maxVal.Int64)
	if err != nil {
		return err
	}
	if !ok || gap <= 0 {
		return nil
	}
	if gap > MaxAutoNumberGap {
		return fmt.Errorf(
			"column %q.%q's auto-number sequence is too far behind its data (gap of %d) to re-seed safely; use a plain Integer column instead of auto-number for ids this large",
			table, column, gap)
	}

	seq := AutoNumberSequence(table, column)
	seqRef := session.TableRef{Catalog: ref.Catalog, Schema: ref.Schema, Name: seq}
	var cur int64
	if err := q.QueryRowContext(ctx,
		fmt.Sprintf("SELECT max(nextval(%s)) FROM range(%d)", session.QuoteLiteral(seqRef.SQL()), gap),
	).Scan(&cur); err != nil {
		return fmt.Errorf("failed to advance sequence %q: %w", seq, err)
	}
	return nil
}

var nextvalLiteralRE = regexp.MustCompile(`(?i)nextval\(\s*'((?:[^']|'')*)'`)

// defaultUsesSequence reports whether a column default is nextval of exactly
// the sequence named seq (any catalog/schema qualification).
func defaultUsesSequence(def, seq string) bool {
	m := nextvalLiteralRE.FindStringSubmatch(def)
	if m == nil {
		return false
	}
	ref, err := session.ParseTableRef(strings.ReplaceAll(m[1], "''", "'"))
	return err == nil && strings.EqualFold(ref.Name, seq)
}
