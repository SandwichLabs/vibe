package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/SandwichLabs/vibe/internal/agents"
	"github.com/SandwichLabs/vibe/internal/importer"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ExportQueryResults writes the result of a SQL query to CSV or Parquet via
// DuckDB COPY (W5). format is "csv" or "parquet". dst is the destination path;
// when empty, a native save dialog is shown (the UI path) — a non-empty dst
// writes directly (scriptable/testable, mirroring ExportTableXLSX). Returns
// the written path, or "" if the dialog was cancelled.
func (a *App) ExportQueryResults(sqlStr, format, dst string) (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.requireWorkspace(); err != nil {
		return "", err
	}
	if strings.TrimSpace(sqlStr) == "" {
		return "", fmt.Errorf("no query to export")
	}

	var ext, copyOpts string
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "csv":
		ext, copyOpts = ".csv", "FORMAT csv, HEADER true"
	case "parquet":
		ext, copyOpts = ".parquet", "FORMAT parquet"
	default:
		return "", fmt.Errorf("unsupported export format %q (want csv or parquet)", format)
	}

	if dst == "" {
		path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
			Title:           "Export query results",
			DefaultFilename: "query" + ext,
			Filters: []runtime.FileFilter{
				{DisplayName: strings.ToUpper(format) + " Files (*" + ext + ")", Pattern: "*" + ext},
			},
		})
		if err != nil {
			return "", fmt.Errorf("failed to open save dialog: %w", err)
		}
		if path == "" {
			return "", nil
		}
		dst = path
	}
	if !strings.HasSuffix(strings.ToLower(dst), ext) {
		dst += ext
	}

	ddb := a.sess.DB()
	if ddb == nil {
		return "", fmt.Errorf("DuckDB not available")
	}
	copySQL := buildCopySQL(sqlStr, dst, copyOpts)
	if _, err := ddb.Exec(copySQL); err != nil {
		return "", fmt.Errorf("failed to export query results: %w", err)
	}
	a.logger.Info("exported query results", "format", format, "path", dst)
	return dst, nil
}

// --- Import/Export ---

// SelectImportFile opens a file dialog to select a data file for import
// (CSV/TSV or Excel). Replaces SelectCSVFile and the old
// SelectSpreadsheetFile stub (which always returned "not implemented" —
// the xlsx Browse button was broken in the real app before this).
func (a *App) SelectImportFile() (string, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Import data",
		Filters: []runtime.FileFilter{
			{DisplayName: "Data files (*.csv, *.tsv, *.xlsx)", Pattern: "*.csv;*.tsv;*.xlsx"},
			{DisplayName: "CSV Files (*.csv)", Pattern: "*.csv"},
			{DisplayName: "Excel Files (*.xlsx)", Pattern: "*.xlsx"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to open file dialog: %w", err)
	}
	return path, nil
}

// InspectImport stages the whole file and returns its columns (with
// inferred types and primary-key candidates), a preview of the first 50
// rows, and the total row count — the data behind the wizard's preview
// grid. Read-only: it never writes to the container, only to the DuckDB
// session's own temp catalog, so it takes RLock like any other query.
func (a *App) InspectImport(src importer.Source) (*importer.Inspection, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	db := a.sess.DB()
	if db == nil {
		return nil, fmt.Errorf("DuckDB not available")
	}
	return importer.Inspect(a.ctx, db, src)
}

// ImportFile runs one import per the supplied options — create, replace,
// append, or upsert — pre-flighted and written atomically by
// importer.Import. Replaces ImportCSV, ImportSpreadsheet, and
// PreviewSpreadsheet.
//
// A dry run (o.DryRun) only writes to the DuckDB session's temp catalog, so
// it takes RLock; a real run takes the full Lock. Only a real, successful
// run (!DryRun && summary.OK) marks the file dirty, refreshes the
// workspace-<table> skill, and emits data:changed — a dry run or a
// pre-flight failure leaves the file exactly as it was.
func (a *App) ImportFile(o importer.Options) (*importer.Summary, error) {
	// No identifier check here: importer.Import validates o.Table
	// itself (engine.ValidIdentifier)
	// and returns a clean "invalid table name" error — a second
	// check here would just be two call sites that could quietly diverge.
	if o.DryRun {
		a.mu.RLock()
		defer a.mu.RUnlock()
	} else {
		a.mu.Lock()
		defer a.mu.Unlock()
	}

	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	db := a.sess.DB()
	if db == nil {
		return nil, fmt.Errorf("DuckDB not available")
	}

	summary, err := importer.Import(a.ctx, db, o)
	if err != nil {
		return nil, err
	}

	if !o.DryRun && summary.OK {
		// The write already landed atomically inside importer.Import's own
		// transaction — there is no "mark dirty as writes begin" window to
		// worry about here, unlike the old per-row importer.
		// summary.Table, not o.Table: for append/upsert/replace into an
		// existing table, Import resolves and returns the CATALOG's own
		// spelling (a case-insensitive match, e.g. o.Table="customers"
		// against a table actually named "Customers") — writing the skill
		// under the caller's original casing could create a second,
		// wrong-table skill instead of refreshing the real one's.
		if err := agents.WriteImportSkill(a.ws.Skills(), a.sess, summary.Table); err != nil {
			a.logger.Warn("failed to write import skill", "table", summary.Table, "error", err)
		}
		a.emitEvent("data:changed")
	}

	return summary, nil
}

// --- Helpers ---

// buildCopySQL wraps a user/saved query in COPY (…) TO '<dst>' WITH
// (<opts>). Trailing whitespace and semicolons are stripped (a query that
// runs fine with a trailing ";" must export too — DuckDB rejects a second
// statement inside the COPY's parenthesized subquery), and the closing
// paren goes on its own line so a trailing "-- comment" in query can't
// swallow it. dst's single quotes are doubled so a path containing one
// stays a single, well-formed string literal.
//
// This does not try to parse comments out of query: a "SELECT 1; -- x"
// still fails, and DuckDB's own parser error says so — good enough, and
// safer than attempting to strip comments ourselves.
func buildCopySQL(query, dst, opts string) string {
	query = strings.TrimRight(query, " \t\r\n;")
	return fmt.Sprintf("COPY (%s\n) TO '%s' WITH (%s)",
		query, strings.ReplaceAll(dst, "'", "''"), opts)
}

// copyToXLSX writes the result of the given SELECT source to an .xlsx via the
// DuckDB excel extension (fetched once on first use). It never falls back to another format: failure is an error.
// It returns the written .xlsx path. Caller must hold a.mu (R or W).
func (a *App) copyToXLSX(source, dst string) (string, error) {
	ddb := a.sess.DB()
	if ddb == nil {
		return "", fmt.Errorf("DuckDB not available")
	}

	// The excel extension is not autoloaded (the hub runs hardened, with an
	// app-private extension directory): fetch and load it explicitly. A
	// failure is returned to the user; the export never silently becomes a
	// different format.
	if err := session.EnsureExtension(a.ctx, ddb, "excel", "Exporting to Excel"); err != nil {
		return "", err
	}

	// COPY (query) TO 'file.xlsx' WITH (FORMAT xlsx). Escape single quotes in the
	// destination path to keep the SQL literal well-formed.
	xlsxPath := dst
	if !strings.HasSuffix(strings.ToLower(xlsxPath), ".xlsx") {
		xlsxPath += ".xlsx"
	}
	copyXLSX := buildCopySQL(source, xlsxPath, "FORMAT xlsx, HEADER true")
	if _, err := ddb.ExecContext(a.ctx, copyXLSX); err != nil {
		return "", fmt.Errorf("failed to export to Excel: %w", err)
	}
	return xlsxPath, nil
}

// ExportTableXLSX exports an arbitrary table to an .xlsx via the DuckDB excel
// extension (an error if it cannot be fetched), returning the written path
// (plan §6.2 / P6.5).
func (a *App) ExportTableXLSX(tableName, dst string) (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.requireWorkspace(); err != nil {
		return "", err
	}
	if tableName == "" {
		return "", fmt.Errorf("table name is required")
	}

	// Quote the identifier to handle reserved words / case; escape embedded quotes.
	source := fmt.Sprintf(`SELECT * FROM "%s"`, strings.ReplaceAll(tableName, `"`, `""`))
	path, err := a.copyToXLSX(source, dst)
	if err != nil {
		return "", fmt.Errorf("failed to export table %q to spreadsheet: %w", tableName, err)
	}
	a.logger.Info("exported table spreadsheet", "table", tableName, "path", path)
	return path, nil
}

// ExportQueryXLSX exports a saved query's result to an .xlsx via the DuckDB excel
// extension (an error if it cannot be fetched), returning the written path
// (plan §6.2 / P6.5).
func (a *App) ExportQueryXLSX(queryID, dst string) (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if err := a.requireWorkspace(); err != nil {
		return "", err
	}

	q, err := a.ws.Queries().Get(context.Background(), queryID)
	if err != nil {
		return "", fmt.Errorf("failed to load query %q: %w", queryID, err)
	}

	path, err := a.copyToXLSX(q.SQL, dst)
	if err != nil {
		return "", fmt.Errorf("failed to export query %q to spreadsheet: %w", queryID, err)
	}
	a.logger.Info("exported query spreadsheet", "query", queryID, "path", path)
	return path, nil
}

// AnalyzeQueryInExcel is the one-click "Analyze in Excel/Sheets" alias for
// ExportQueryXLSX (plan §6.2 / P6.5).
func (a *App) AnalyzeQueryInExcel(queryID, dst string) (string, error) {
	return a.ExportQueryXLSX(queryID, dst)
}
