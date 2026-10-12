package importer

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/session"
)

// writeCreate creates a fresh table from target and inserts every staged
// row into it, all inside one transaction. key selects the primary key:
// KeyNone for none, KeyAuto for a generated auto-number "id" column, or a
// target column name for an integer/text key (integer keys get an
// auto-number sequence default, matching CreateTable/backfillAutoNumberPKs).
func writeCreate(ctx context.Context, conn *sql.Conn, table string, target []targetCol, key string, rowCount int) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	if err := createAndInsert(ctx, tx, table, target, key, rowCount); err != nil {
		tx.Rollback() //nolint:errcheck
		return err
	}
	if beforeCommit != nil {
		if err := beforeCommit(tx); err != nil {
			tx.Rollback() //nolint:errcheck
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}
	return nil
}

// writeReplace drops the existing table (and any auto-number sequences it
// used) and recreates it from target, all inside one transaction — DuckDB
// refuses the DROP outright when another table has an incoming FK to it, so
// by the time we get here (past preflightCreate's caller checking
// allowedModes) that has already been ruled out.
func writeReplace(ctx context.Context, conn *sql.Conn, table string, existing *existingTable, target []targetCol, key string, rowCount int) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("DROP TABLE %s", tableSQL(ctx, table))); err != nil {
		tx.Rollback() //nolint:errcheck
		return fmt.Errorf("failed to drop %q: %w", table, err)
	}
	for _, c := range existing.columns {
		seq := engine.AutoNumberSequence(table, c.Name)
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("DROP SEQUENCE IF EXISTS %s", session.QuoteIdent(seq))); err != nil {
			tx.Rollback() //nolint:errcheck
			return fmt.Errorf("failed to drop sequence %q: %w", seq, err)
		}
	}
	if err := createAndInsert(ctx, tx, table, target, key, rowCount); err != nil {
		tx.Rollback() //nolint:errcheck
		return err
	}
	if beforeCommit != nil {
		if err := beforeCommit(tx); err != nil {
			tx.Rollback() //nolint:errcheck
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}
	return nil
}

// createAndInsert is the CREATE TABLE + INSERT shared by writeCreate and
// writeReplace (identical once the old table, if any, is out of the way).
func createAndInsert(ctx context.Context, tx *sql.Tx, table string, target []targetCol, key string, rowCount int) error {
	var colDefs []string
	var selectExprs []string
	var colNames []string

	keyIsInteger := false
	if key == KeyAuto {
		autoName := "id"
		used := map[string]bool{}
		for _, tc := range target {
			used[tc.Name] = true
		}
		for i := 2; used[autoName]; i++ {
			autoName = fmt.Sprintf("id_%d", i)
		}
		seq := engine.AutoNumberSequence(table, autoName)
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("CREATE SEQUENCE %s START %d", session.QuoteIdent(seq), rowCount+1)); err != nil {
			return fmt.Errorf("failed to create id sequence: %w", err)
		}
		colDefs = append(colDefs, fmt.Sprintf("%s BIGINT PRIMARY KEY DEFAULT nextval(%s)", session.QuoteIdent(autoName), session.QuoteLiteral(seq)))
		colNames = append(colNames, session.QuoteIdent(autoName))
		selectExprs = append(selectExprs, "__row")
	} else if key != KeyNone {
		for _, tc := range target {
			if tc.Name == key && tc.Friendly.Type == TypeInteger {
				keyIsInteger = true
			}
		}
		if keyIsInteger {
			seq := engine.AutoNumberSequence(table, key)
			var maxQuery = fmt.Sprintf("SELECT COALESCE(MAX(TRY_CAST(%s AS BIGINT)), 0) + 1 FROM %s",
				session.QuoteIdent(findStagedSource(target, key)), stageTable)
			var start int64
			if err := tx.QueryRowContext(ctx, maxQuery).Scan(&start); err != nil {
				return fmt.Errorf("failed to seed key sequence: %w", err)
			}
			if _, err := tx.ExecContext(ctx, fmt.Sprintf("CREATE SEQUENCE %s START %d", session.QuoteIdent(seq), start)); err != nil {
				return fmt.Errorf("failed to create key sequence: %w", err)
			}
		}
	}

	for _, tc := range target {
		def := fmt.Sprintf("%s %s", session.QuoteIdent(tc.Name), tc.Friendly.DuckDB())
		if tc.Name == key && keyIsInteger {
			def += fmt.Sprintf(" DEFAULT nextval('%s')", engine.AutoNumberSequence(table, key))
		}
		if tc.Name == key {
			def += " PRIMARY KEY"
		}
		colDefs = append(colDefs, def)
		colNames = append(colNames, session.QuoteIdent(tc.Name))
		value, _ := castExpr(session.QuoteIdent(tc.Staged.SourceName), tc.Staged.StagedType, tc.Friendly)
		selectExprs = append(selectExprs, value)
	}

	createSQL := fmt.Sprintf("CREATE TABLE %s (%s)", tableSQL(ctx, table), strings.Join(colDefs, ", "))
	if _, err := tx.ExecContext(ctx, createSQL); err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	insertSQL := fmt.Sprintf("INSERT INTO %s (%s) SELECT %s FROM %s ORDER BY __row",
		tableSQL(ctx, table), strings.Join(colNames, ", "), strings.Join(selectExprs, ", "), stageTable)
	if _, err := tx.ExecContext(ctx, insertSQL); err != nil {
		return fmt.Errorf("failed to insert rows: %w", err)
	}
	return nil
}

// keyValueExpr is keyCastExpr's value half, with the staged source column
// reference qualified (e.g. "s." in a correlated subquery/join) instead of
// bare — used wherever a key comparison runs against an aliased stage
// table rather than the whole table.
func keyValueExpr(tc *targetCol, qualifier string) string {
	col := qualifier + session.QuoteIdent(tc.Staged.SourceName)
	var value string
	if tc.HasFriendly {
		value, _ = castExpr(col, tc.Staged.StagedType, tc.Friendly)
	} else {
		value, _ = castExprRaw(col, tc.RawType)
	}
	return value
}

// findStagedSource looks up the staged (file-header) source name for a
// target column by its normalized name, falling back to name itself if
// there's no match (there always is, for every caller: create/replace's
// own key is one of target's own columns) — not a "must" that panics.
func findStagedSource(target []targetCol, name string) string {
	for _, tc := range target {
		if tc.Name == name && tc.Staged != nil {
			return tc.Staged.SourceName
		}
	}
	return name
}

// writeAppendOrUpsert inserts matched staged rows into the existing table,
// either as a plain append or as an upsert (ON CONFLICT (pk) DO UPDATE …
// WHERE the non-key columns actually differ, so a re-import of unchanged
// data affects zero rows). It returns the number of rows the INSERT
// affected (append: rows inserted; upsert: rows inserted+updated, though
// callers get the inserted/updated/unchanged breakdown from countUpsert
// instead). Afterwards it bumps any integer auto-number PK past the new
// max, matching writeCreate/writeReplace's auto-numbering.
func writeAppendOrUpsert(ctx context.Context, conn *sql.Conn, table string, existing *existingTable, target []targetCol, upsert bool) (int, error) {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}

	insertSQL := buildInsertSQL(ctx, table, existing, target, upsert)
	res, err := tx.ExecContext(ctx, insertSQL)
	if err != nil {
		tx.Rollback() //nolint:errcheck
		return 0, fmt.Errorf("failed to insert rows: %w", err)
	}
	n, _ := res.RowsAffected()

	if err := bumpKeySequence(ctx, tx, table, existing, target); err != nil {
		tx.Rollback() //nolint:errcheck
		return 0, err
	}

	if beforeCommit != nil {
		if err := beforeCommit(tx); err != nil {
			tx.Rollback() //nolint:errcheck
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit: %w", err)
	}
	return int(n), nil
}

// buildInsertSQL is the INSERT ... SELECT from the stage table for the
// matched columns, with the ON CONFLICT clause when upserting.
func buildInsertSQL(ctx context.Context, table string, existing *existingTable, target []targetCol, upsert bool) string {
	var colNames, selectExprs []string
	for _, tc := range target {
		if tc.Staged == nil {
			continue // no DEFAULT-having column absent from the file: leave it to its DEFAULT
		}
		colNames = append(colNames, session.QuoteIdent(tc.Name))
		var value string
		if tc.HasFriendly {
			value, _ = castExpr(session.QuoteIdent(tc.Staged.SourceName), tc.Staged.StagedType, tc.Friendly)
		} else {
			value, _ = castExprRaw(session.QuoteIdent(tc.Staged.SourceName), tc.RawType)
		}
		selectExprs = append(selectExprs, value)
	}

	insertSQL := fmt.Sprintf("INSERT INTO %s (%s) SELECT %s FROM %s ORDER BY __row",
		tableSQL(ctx, table), strings.Join(colNames, ", "), strings.Join(selectExprs, ", "), stageTable)
	if !upsert {
		return insertSQL
	}

	var setParts []string
	var distinctOld, distinctNew []string
	for _, tc := range target {
		if tc.Name == existing.pkCol || tc.Staged == nil {
			continue
		}
		q := session.QuoteIdent(tc.Name)
		setParts = append(setParts, fmt.Sprintf("%s = excluded.%s", q, q))
		distinctOld = append(distinctOld, session.QuoteIdent(table)+"."+q)
		distinctNew = append(distinctNew, "excluded."+q)
	}
	insertSQL += fmt.Sprintf(" ON CONFLICT (%s) ", session.QuoteIdent(existing.pkCol))
	if len(setParts) == 0 {
		return insertSQL + "DO NOTHING"
	}
	return insertSQL + fmt.Sprintf("DO UPDATE SET %s WHERE (%s) IS DISTINCT FROM (%s)",
		strings.Join(setParts, ", "), strings.Join(distinctOld, ", "), strings.Join(distinctNew, ", "))
}

// bumpKeySequence advances an integer auto-number key past the inserted rows.
func bumpKeySequence(ctx context.Context, tx *sql.Tx, table string, existing *existingTable, target []targetCol) error {
	if existing.pkCol == "" {
		return nil
	}
	for _, tc := range target {
		// Only bump when the file supplied explicit key values: when
		// the key is absent from the file (a plain append relying on
		// the column's own DEFAULT), every inserted row already
		// consumed nextval() through that DEFAULT and the sequence is
		// already correct — bumping again here would open an
		// unnecessary gap.
		if tc.Name == existing.pkCol && tc.HasFriendly && tc.Friendly.Type == TypeInteger && tc.Staged != nil {
			if err := engine.BumpAutoNumber(ctx, tx, session.TableRef{Name: table}, existing.pkCol); err != nil {
				return fmt.Errorf("failed to advance key sequence: %w", err)
			}
		}
	}
	return nil
}

// countUpsert computes the Inserted/Updated/Unchanged breakdown an upsert
// will produce, without writing anything — used both for DryRun and for the
// real run's summary (the write itself doesn't distinguish inserted from
// updated rows in its affected-row count).
func countUpsert(ctx context.Context, conn *sql.Conn, table string, existing *existingTable, target []targetCol) (inserted, updated, unchanged int, err error) {
	var keyStaged *targetCol
	var nonKey []targetCol
	for i := range target {
		if target[i].Name == existing.pkCol {
			keyStaged = &target[i]
			continue
		}
		if target[i].Staged != nil {
			nonKey = append(nonKey, target[i])
		}
	}
	if keyStaged == nil || keyStaged.Staged == nil {
		return 0, 0, 0, fmt.Errorf("upsert requires the key column in the file")
	}
	// The key's CAST value (what the write actually inserts), not the raw
	// staged text — see keyCastExpr's doc comment: comparing raw text would
	// miss that "1" and "01" are the same BIGINT key, and fails outright
	// when the key is text-staged but the table's PK is an integer.
	keyValue := keyValueExpr(keyStaged, "s.")
	tableKeyCol := session.QuoteIdent(existing.pkCol)

	q := fmt.Sprintf("SELECT COUNT(*) FROM %s s WHERE NOT EXISTS (SELECT 1 FROM %s t WHERE t.%s = %s)",
		stageTable, tableSQL(ctx, table), tableKeyCol, keyValue)
	if err := conn.QueryRowContext(ctx, q).Scan(&inserted); err != nil {
		return 0, 0, 0, fmt.Errorf("failed to count new rows: %w", err)
	}

	if len(nonKey) == 0 {
		matching := 0
		q2 := fmt.Sprintf("SELECT COUNT(*) FROM %s s WHERE EXISTS (SELECT 1 FROM %s t WHERE t.%s = %s)",
			stageTable, tableSQL(ctx, table), tableKeyCol, keyValue)
		if err := conn.QueryRowContext(ctx, q2).Scan(&matching); err != nil {
			return 0, 0, 0, err
		}
		return inserted, 0, matching, nil
	}

	var oldExprs, newExprs []string
	for _, tc := range nonKey {
		q := session.QuoteIdent(tc.Name)
		srcCol := "s." + session.QuoteIdent(tc.Staged.SourceName)
		var value string
		if tc.HasFriendly {
			value, _ = castExpr(srcCol, tc.Staged.StagedType, tc.Friendly)
		} else {
			value, _ = castExprRaw(srcCol, tc.RawType)
		}
		oldExprs = append(oldExprs, "t."+q)
		newExprs = append(newExprs, value)
	}
	q3 := fmt.Sprintf(
		`SELECT
			COUNT(*) FILTER (WHERE (%s) IS DISTINCT FROM (%s)) AS changed,
			COUNT(*) FILTER (WHERE (%s) IS NOT DISTINCT FROM (%s)) AS same
		 FROM %s s JOIN %s t ON t.%s = %s`,
		strings.Join(oldExprs, ", "), strings.Join(newExprs, ", "),
		strings.Join(oldExprs, ", "), strings.Join(newExprs, ", "),
		stageTable, tableSQL(ctx, table), tableKeyCol, keyValue)
	if err := conn.QueryRowContext(ctx, q3).Scan(&updated, &unchanged); err != nil {
		return 0, 0, 0, fmt.Errorf("failed to count changed rows: %w", err)
	}
	return inserted, updated, unchanged, nil
}
