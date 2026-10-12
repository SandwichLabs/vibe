package importer

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/session"
)

// maxBadValues bounds how many pre-flight problems Summary.BadValues carries
// (the UI/CLI show "and N more" from BadCount).
const maxBadValues = 10

// typeLabel is the friendly, plain-words name for a ColType, used in
// BadValue.Want and (mirrored in TS) the wizard's type dropdown.
func typeLabel(ct ColType) string {
	switch ct.Type {
	case TypeText:
		return "Text"
	case TypeInteger:
		return "Integer"
	case TypeDecimal:
		return fmt.Sprintf("Decimal (%d places)", ct.Scale)
	case TypeNumber:
		return "Number"
	case TypeDate:
		return "Date"
	case TypeTimestamp:
		return "Date & time"
	case TypeBoolean:
		return "True/False"
	default:
		return string(ct.Type)
	}
}

// fitsScan runs a lossless-round-trip check (castExpr/castExprRaw's fits)
// over every target column that has a matching staged source column,
// returning the first maxBadValues failures and the total failure count.
func fitsScan(ctx context.Context, conn *sql.Conn, target []targetCol) ([]BadValue, int, error) {
	var parts []string
	for _, tc := range target {
		if tc.Staged == nil {
			continue
		}
		col := session.QuoteIdent(tc.Staged.SourceName)
		var fits string
		if tc.HasFriendly {
			_, fits = castExpr(col, tc.Staged.StagedType, tc.Friendly)
		} else {
			_, fits = castExprRaw(col, tc.RawType)
		}
		want := typeLabel(tc.Friendly)
		if !tc.HasFriendly {
			want = tc.RawType
		}
		parts = append(parts, fmt.Sprintf(
			"SELECT __row AS row, %s AS col, CAST(%s AS VARCHAR) AS val, %s AS want FROM %s WHERE NOT (%s)",
			session.QuoteLiteral(tc.Name), col, session.QuoteLiteral(want), stageTable, fits))
	}
	if len(parts) == 0 {
		return nil, 0, nil
	}
	union := strings.Join(parts, " UNION ALL ")

	var total int
	if err := conn.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM (%s) t", union)).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to check values: %w", err)
	}
	if total == 0 {
		return nil, 0, nil
	}

	rows, err := conn.QueryContext(ctx, fmt.Sprintf("SELECT row, col, val, want FROM (%s) t ORDER BY row LIMIT %d", union, maxBadValues))
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list bad values: %w", err)
	}
	defer rows.Close()

	var out []BadValue
	for rows.Next() {
		var bv BadValue
		var val sql.NullString
		if err := rows.Scan(&bv.Row, &bv.Column, &val, &bv.Want); err != nil {
			return nil, 0, fmt.Errorf("failed to scan bad value: %w", err)
		}
		bv.Value = val.String
		bv.Reason = "type"
		out = append(out, bv)
	}
	return out, total, rows.Err()
}

// keyCastExpr returns the value expression a key target column will
// actually be WRITTEN as (the same one castExpr/castExprRaw gives the real
// write path) and a display expression (the original source text) for
// BadValue.Value. Every key check (keyProblems, an existing-row collision
// check, countUpsert's join) must compare THIS value, not the raw staged
// text — otherwise "1" and "01" look like different keys right up until
// the INSERT casts them both to the same BIGINT 1 and DuckDB's ON CONFLICT
// silently collapses them, and a text-staged key can't even be compared against
// an integer table column at all without an explicit cast (a raw
// "Cannot compare VARCHAR and BIGINT" error otherwise).
func keyCastExpr(tc *targetCol) (valueExpr, displayExpr string) {
	col := session.QuoteIdent(tc.Staged.SourceName)
	if tc.HasFriendly {
		valueExpr, _ = castExpr(col, tc.Staged.StagedType, tc.Friendly)
	} else {
		valueExpr, _ = castExprRaw(col, tc.RawType)
	}
	return valueExpr, fmt.Sprintf("CAST(%s AS VARCHAR)", col)
}

// keyProblems checks a chosen key's cast value (keyExpr — see keyCastExpr;
// NOT the raw staged column) for NULLs and in-file duplicates (DuckDB's ON
// CONFLICT silently collapses duplicate source keys instead of erroring —
// so this pre-check is mandatory, not a
// "simplify this away" candidate). displayExpr renders the ORIGINAL text
// for BadValue.Value (keyExpr may itself be NULL for a value that doesn't
// even cast, which fitsScan already reports separately as a "type"
// problem). reportCol is the column name attached to each BadValue — the
// target/normalized name, matching fitsScan's own BadValue.Column, not
// necessarily the identifier keyExpr references. remaining caps how many
// additional BadValue rows may be appended (the overall list is capped at
// maxBadValues).
func keyProblems(ctx context.Context, conn *sql.Conn, keyExpr, displayExpr, reportCol string, remaining int) (rows []BadValue, count int, err error) {
	var nullCount, dupCount int
	if err := conn.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s IS NULL", stageTable, keyExpr)).Scan(&nullCount); err != nil {
		return nil, 0, fmt.Errorf("failed to check key nulls: %w", err)
	}
	if err := conn.QueryRowContext(ctx, fmt.Sprintf(
		`SELECT COALESCE(SUM(c - 1), 0) FROM (SELECT COUNT(*) AS c FROM %s WHERE %s IS NOT NULL GROUP BY %s HAVING COUNT(*) > 1)`,
		stageTable, keyExpr, keyExpr)).Scan(&dupCount); err != nil {
		return nil, 0, fmt.Errorf("failed to check key duplicates: %w", err)
	}
	count = nullCount + dupCount
	if count == 0 || remaining <= 0 {
		return nil, count, nil
	}

	if nullCount > 0 {
		nrows, err := conn.QueryContext(ctx, fmt.Sprintf(
			"SELECT __row, %s FROM %s WHERE %s IS NULL ORDER BY __row LIMIT %d", displayExpr, stageTable, keyExpr, remaining))
		if err != nil {
			return nil, 0, err
		}
		for nrows.Next() && len(rows) < remaining {
			var r int
			var v sql.NullString
			if err := nrows.Scan(&r, &v); err != nil {
				nrows.Close()
				return nil, 0, err
			}
			rows = append(rows, BadValue{Row: r, Column: reportCol, Value: v.String, Reason: "null_key"})
		}
		nrows.Close()
	}
	if dupCount > 0 && len(rows) < remaining {
		drows, err := conn.QueryContext(ctx, fmt.Sprintf(
			`SELECT __row, %s FROM %s WHERE %s IN
			   (SELECT %s FROM %s WHERE %s IS NOT NULL GROUP BY %s HAVING COUNT(*) > 1)
			 ORDER BY __row LIMIT %d`, displayExpr, stageTable, keyExpr, keyExpr, stageTable, keyExpr, keyExpr, remaining-len(rows)))
		if err != nil {
			return nil, 0, err
		}
		for drows.Next() && len(rows) < remaining {
			var r int
			var v sql.NullString
			if err := drows.Scan(&r, &v); err != nil {
				drows.Close()
				return nil, 0, err
			}
			rows = append(rows, BadValue{Row: r, Column: reportCol, Value: v.String, Reason: "duplicate_key"})
		}
		drows.Close()
	}
	return rows, count, nil
}

// fkOrphans checks each of the target's outgoing single-column foreign keys
// for file values with no matching row in the referenced table/column.
func fkOrphans(ctx context.Context, conn *sql.Conn, target []targetCol, fks []fkInfo, remaining int) (rows []BadValue, count int, err error) {
	for _, fk := range fks {
		var tc *targetCol
		for i := range target {
			if target[i].Name == fk.Column && target[i].Staged != nil {
				tc = &target[i]
				break
			}
		}
		if tc == nil {
			continue
		}
		srcCol := session.QuoteIdent(tc.Staged.SourceName)
		refExpr := fmt.Sprintf("%s.%s", session.QuoteIdent(fk.RefTable), session.QuoteIdent(fk.RefColumn))
		orphanWhere := fmt.Sprintf(
			"%s IS NOT NULL AND NOT EXISTS (SELECT 1 FROM %s WHERE %s = %s)",
			srcCol, session.QuoteIdent(fk.RefTable), refExpr, srcCol)

		var n int
		if err := conn.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s", stageTable, orphanWhere)).Scan(&n); err != nil {
			return nil, 0, fmt.Errorf("failed to check foreign key %q: %w", fk.Column, err)
		}
		count += n
		if n == 0 || len(rows) >= remaining {
			continue
		}
		orows, err := conn.QueryContext(ctx, fmt.Sprintf(
			"SELECT __row, CAST(%s AS VARCHAR) FROM %s WHERE %s ORDER BY __row LIMIT %d",
			srcCol, stageTable, orphanWhere, remaining-len(rows)))
		if err != nil {
			return nil, 0, err
		}
		for orows.Next() && len(rows) < remaining {
			var r int
			var v sql.NullString
			if err := orows.Scan(&r, &v); err != nil {
				orows.Close()
				return nil, 0, err
			}
			rows = append(rows, BadValue{Row: r, Column: fk.Column, Value: v.String, Want: fk.RefTable, Reason: "missing_parent"})
		}
		orows.Close()
	}
	return rows, count, nil
}

// preflightCreate pre-flights create/replace: a fits scan over every column,
// plus a NULL/duplicate check on the chosen key.
func preflightCreate(ctx context.Context, conn *sql.Conn, sum *Summary, target []targetCol, key string) error {
	bad, badCount, err := fitsScan(ctx, conn, target)
	if err != nil {
		return err
	}
	sum.BadValues, sum.BadCount = bad, badCount

	if key != KeyAuto && key != KeyNone {
		var keyTarget *targetCol
		for i := range target {
			if target[i].Name == key {
				keyTarget = &target[i]
				break
			}
		}
		if keyTarget == nil || keyTarget.Staged == nil {
			sum.Blocked = fmt.Sprintf("primary key %q is not a column in this file", key)
			return nil
		}
		// keyTarget.Name (the normalized target identifier, e.g. "order_id")
		// is NOT a column of the stage table — the stage table's columns
		// keep the file's own header ("Order ID"). Querying the stage
		// table by the normalized name is exactly the bug that gave a raw
		// "Referenced column ... not found" the moment a key header wasn't
		// already a valid identifier; keyCastExpr resolves the actual
		// staged source column instead.
		keyExpr, displayExpr := keyCastExpr(keyTarget)
		remaining := maxBadValues - len(sum.BadValues)
		krows, kcount, err := keyProblems(ctx, conn, keyExpr, displayExpr, key, remaining)
		if err != nil {
			return err
		}
		if kcount > 0 {
			sum.BadValues = append(sum.BadValues, krows...)
			sum.BadCount += kcount
			sum.Blocked = fmt.Sprintf("the %q column has missing or duplicate values and can't be the primary key", key)
		}
	}

	sum.OK = sum.BadCount == 0 && sum.Blocked == ""
	return nil
}

// preflightAppend pre-flights append: see preflightAppendOrUpsert.
func preflightAppend(ctx context.Context, conn *sql.Conn, sum *Summary, existing *existingTable, target []targetCol) error {
	return preflightAppendOrUpsert(ctx, conn, sum, existing, target, false)
}

// preflightUpsert pre-flights upsert: see preflightAppendOrUpsert.
func preflightUpsert(ctx context.Context, conn *sql.Conn, sum *Summary, existing *existingTable, target []targetCol) error {
	return preflightAppendOrUpsert(ctx, conn, sum, existing, target, true)
}

// preflightAppendOrUpsert is append and upsert's shared pre-flight: missing/
// extra columns block outright; otherwise a fits scan and FK-orphan check,
// identical for both modes. They differ only in what they do with the
// table's existing key (if any):
//   - upsert always has one (allowedModes only offers upsert when the PK is
//     present in the file) and only needs it free of NULLs/in-file
//     duplicates — a key that already exists in the table is the whole
//     point of upsert, not a problem.
//   - append blocks outright on any row whose key already exists (pointing
//     at "Update matching rows" instead), and only THEN checks the
//     remaining new rows for in-file NULLs/duplicates. Without a PK at
//     all, in-file-exact-duplicate rows are only ever a warning
//     (ExactDupes on sum), never a block.
//
// Every key check compares the key's CAST value (keyCastExpr), never the
// raw staged text — see keyCastExpr's doc comment for why.
func preflightAppendOrUpsert(ctx context.Context, conn *sql.Conn, sum *Summary, existing *existingTable, target []targetCol, upsert bool) error {
	if len(sum.Missing) > 0 || len(sum.Extra) > 0 {
		return nil
	}

	if err := scanValueProblems(ctx, conn, sum, existing, target); err != nil {
		return err
	}

	var keyTarget *targetCol
	for i := range target {
		if target[i].Name == existing.pkCol {
			keyTarget = &target[i]
			break
		}
	}

	if keyTarget == nil || keyTarget.Staged == nil {
		// upsert always has a matched key (see doc comment); append
		// without a matched key — whether the table has no PK at all, OR
		// it has one but the file doesn't supply it (an auto-number
		// table, which is exactly the case a re-import of the same
		// no-id file hits) — falls back to the exact-duplicate warning.
		// Checking this ONLY for existing.pkCol == "" (the original
		// version) silently duplicated every row of an auto-key table on
		// re-import: nothing else here would ever catch it, since there
		// is no key at all to collide on.
		if !upsert {
			var err error
			sum.ExactDupes, err = exactDupeCount(ctx, conn, sum.Table, target)
			if err != nil {
				return err
			}
		}
	} else if err := preflightKey(ctx, conn, sum, existing, keyTarget, upsert); err != nil {
		return err
	}

	sum.OK = sum.BadCount == 0 && sum.Blocked == ""
	return nil
}

// scanValueProblems records the values that do not fit their column and the
// rows whose foreign key points at nothing.
func scanValueProblems(ctx context.Context, conn *sql.Conn, sum *Summary, existing *existingTable, target []targetCol) error {
	bad, badCount, err := fitsScan(ctx, conn, target)
	if err != nil {
		return err
	}
	sum.BadValues, sum.BadCount = bad, badCount

	remaining := maxBadValues - len(sum.BadValues)
	fkRows, fkCount, err := fkOrphans(ctx, conn, target, existing.fks, remaining)
	if err != nil {
		return err
	}
	sum.BadValues = append(sum.BadValues, fkRows...)
	sum.BadCount += fkCount
	return nil
}

// preflightKey checks the table's key as the file supplies it: an append is
// blocked by any key already in the table, then both modes are blocked by
// missing or duplicate keys in the file and by an auto-number gap too large to
// re-seed. A block lands in sum.Blocked.
func preflightKey(ctx context.Context, conn *sql.Conn, sum *Summary, existing *existingTable, keyTarget *targetCol, upsert bool) error {
	if !upsert {
		// append: an existing-row collision blocks outright, pointing at
		// upsert instead — duplicates further down the SAME file are a
		// separate, secondary problem only worth reporting once the
		// obvious "wrong mode" case is ruled out.
		keyExpr, _ := keyCastExpr(keyTarget)
		var collide int
		q := fmt.Sprintf("SELECT COUNT(*) FROM %s s WHERE %s IN (SELECT %s FROM %s)",
			stageTable, keyExpr, session.QuoteIdent(existing.pkCol), tableSQL(ctx, sum.Table))
		if err := conn.QueryRowContext(ctx, q).Scan(&collide); err != nil {
			return fmt.Errorf("failed to check existing keys: %w", err)
		}
		if collide > 0 {
			sum.Blocked = fmt.Sprintf("%d row(s) use a %s that's already in the table. Use Update matching rows.", collide, existing.pkCol)
			return nil
		}
	}

	keyExpr, displayExpr := keyCastExpr(keyTarget)
	remaining := maxBadValues - len(sum.BadValues)
	krows, kcount, err := keyProblems(ctx, conn, keyExpr, displayExpr, existing.pkCol, remaining)
	if err != nil {
		return err
	}
	if kcount > 0 {
		sum.BadValues = append(sum.BadValues, krows...)
		sum.BadCount += kcount
		sum.Blocked = fmt.Sprintf("the %q column has missing or duplicate values in the file", existing.pkCol)
		return nil
	}
	if keyTarget.HasFriendly && keyTarget.Friendly.Type == TypeInteger {
		return preflightAutoNumberGap(ctx, conn, sum, existing, keyExpr)
	}
	return nil
}

// preflightAutoNumberGap blocks a file whose keys sit far beyond the table's
// auto-number sequence. It runs BEFORE writing anything: BumpAutoNumber itself
// would refuse a gap this large too, but only after the INSERT already ran
// (inside the write transaction), rolling the whole write back with a thrown
// error instead of a clean, up-front Blocked message. AutoNumberGap never
// touches the sequence, so a block here (or an OK) has zero side effects.
func preflightAutoNumberGap(ctx context.Context, conn *sql.Conn, sum *Summary, existing *existingTable, keyExpr string) error {
	var maxFileKey sql.NullInt64
	q := fmt.Sprintf("SELECT MAX(%s) FROM %s", keyExpr, stageTable)
	if err := conn.QueryRowContext(ctx, q).Scan(&maxFileKey); err != nil {
		return fmt.Errorf("failed to check the new key range: %w", err)
	}
	if !maxFileKey.Valid {
		return nil
	}
	gap, ok, err := engine.AutoNumberGap(ctx, conn, session.TableRef{Name: sum.Table}, existing.pkCol, maxFileKey.Int64)
	if err != nil {
		return err
	}
	if ok && gap > engine.MaxAutoNumberGap {
		sum.Blocked = fmt.Sprintf(
			"row(s) use a %s far beyond the table's auto-number sequence (gap of %d) — this can't be re-seeded safely; use a plain Integer column instead of auto-number for ids this large",
			existing.pkCol, gap)
	}
	return nil
}

// exactDupeCount counts stage rows whose matched-column values already
// exist verbatim in the table (INTERSECT ALL) — a warning only for a
// PK-less append, never a block.
func exactDupeCount(ctx context.Context, conn *sql.Conn, table string, target []targetCol) (int, error) {
	var stageCols, tableCols []string
	for _, tc := range target {
		if tc.Staged == nil {
			continue
		}
		stageCols = append(stageCols, fmt.Sprintf("CAST(%s AS VARCHAR)", session.QuoteIdent(tc.Staged.SourceName)))
		tableCols = append(tableCols, fmt.Sprintf("CAST(%s AS VARCHAR)", session.QuoteIdent(tc.Name)))
	}
	if len(stageCols) == 0 {
		return 0, nil
	}
	q := fmt.Sprintf(
		"SELECT COUNT(*) FROM (SELECT %s FROM %s INTERSECT ALL SELECT %s FROM %s) t",
		strings.Join(stageCols, ", "), stageTable, strings.Join(tableCols, ", "), tableSQL(ctx, table))
	var n int
	if err := conn.QueryRowContext(ctx, q).Scan(&n); err != nil {
		return 0, fmt.Errorf("failed to check duplicate rows: %w", err)
	}
	return n, nil
}
