package notebooks

import (
	"database/sql"
	"log/slog"
)

// MigrateOnOpen runs the one-time page->notebook (#66 part A) and
// report->notebook (#68) migrations that every entry point opening a .vibe
// file must run, exactly once, in the same order, so legacy content becomes
// visible the same way no matter which entry point opened the file: Studio
// (studio/app.go's OpenFile/CreateFile), `vibe mcp serve` (cmd/mcp.go), and
// the TUI (cmd/studio.go). Before this, only Studio called MigratePages/
// MigrateReports — a legacy file opened via `vibe mcp serve` or the TUI kept
// its old pages/reports invisible to list_notebooks, get_notebook, and the
// TUI's notebook list, even though Studio would have migrated the very same
// file.
//
// Both migrations are idempotent (a page/report already stamped
// migrated_to/migrated_to is skipped) and best-effort: a failure is logged
// via logger and never returned as an error, since a migration problem must
// never block opening the file — the original page/report rows are never
// touched beyond stamping migrated_to, so nothing is lost by skipping.
//
// readOnly must be true whenever db belongs to a container opened with
// container.WithReadOnly(true) (`vibe mcp serve --read-only-file`, or any
// future read-only open path). We deliberately SKIP the migration in that
// case rather than "migrate in memory": db is a live connection straight
// into the on-disk .vibe file (container.Open opens the SQLCipher file at
// its real path directly — there is no separate read-only snapshot for the
// SQLite side the way there is for the materialized DuckDB blob), so an
// INSERT into _vibe_notebooks here would write to disk regardless of
// whether Container.Save() is ever called afterwards, silently defeating
// the caller's explicit read-only request. A migration whose result is
// never persisted back into DuckDB/exposed consistently across the process
// lifetime would also be a confusing "sometimes it's there, sometimes it
// isn't" experience. Skipping is simple, honest, and safe: a read-only
// session over a legacy file simply still sees its old pages as pages (not
// notebooks) for that session; open the file normally (read-write) once to
// migrate it permanently.
func MigrateOnOpen(db *sql.DB, readOnly bool, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	if readOnly {
		logger.Info("skipping page/report-to-notebook migration: file opened read-only")
		return
	}

	if n, err := MigratePages(db, logger); err != nil {
		logger.Warn("page-to-notebook migration failed", "error", err)
	} else if n > 0 {
		logger.Info("migrated pages to notebooks", "count", n)
	}

	if n, err := MigrateReports(db, logger); err != nil {
		logger.Warn("report-to-notebook migration failed", "error", err)
	} else if n > 0 {
		logger.Info("migrated reports to notebooks", "count", n)
	}
}
