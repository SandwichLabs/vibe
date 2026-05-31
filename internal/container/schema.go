package container

import (
	"database/sql"
	"fmt"
	"time"
)

// currentSchemaVersion is the latest schema version for the .vibe format.
const currentSchemaVersion = "1"

// schemaDDL contains the CREATE TABLE statements for all _vibe_* tables.
var schemaDDL = []string{
	`CREATE TABLE IF NOT EXISTS _vibe_meta (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);`,

	`CREATE TABLE IF NOT EXISTS _vibe_pages (
		id         TEXT PRIMARY KEY,
		title      TEXT NOT NULL DEFAULT '',
		content    TEXT NOT NULL DEFAULT '',
		page_type  TEXT NOT NULL DEFAULT 'markdown',
		sort_order INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		metadata   TEXT NOT NULL DEFAULT '{}'
	);`,

	`CREATE TABLE IF NOT EXISTS _vibe_queries (
		id         TEXT PRIMARY KEY,
		title      TEXT NOT NULL DEFAULT '',
		sql        TEXT NOT NULL DEFAULT '',
		engine     TEXT NOT NULL DEFAULT 'duckdb',
		parameters TEXT NOT NULL DEFAULT '{}',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);`,

	`CREATE TABLE IF NOT EXISTS _vibe_components (
		id         TEXT PRIMARY KEY,
		type       TEXT NOT NULL DEFAULT '',
		config     TEXT NOT NULL DEFAULT '{}',
		created_at TEXT NOT NULL
	);`,

	`CREATE TABLE IF NOT EXISTS _vibe_sources (
		id          TEXT PRIMARY KEY,
		name        TEXT NOT NULL DEFAULT '',
		source_type TEXT NOT NULL DEFAULT '',
		config      TEXT NOT NULL DEFAULT '{}',
		is_default  INTEGER NOT NULL DEFAULT 0
	);`,

	`CREATE TABLE IF NOT EXISTS _vibe_agent_config (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);`,

	`CREATE TABLE IF NOT EXISTS _vibe_secrets (
		id         TEXT PRIMARY KEY,
		name       TEXT NOT NULL DEFAULT '',
		value      TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL
	);`,

	`CREATE TABLE IF NOT EXISTS _vibe_assets (
		id         TEXT PRIMARY KEY,
		filename   TEXT NOT NULL DEFAULT '',
		mime_type  TEXT NOT NULL DEFAULT 'application/octet-stream',
		data       BLOB,
		created_at TEXT NOT NULL
	);`,

	`CREATE TABLE IF NOT EXISTS _vibe_duckdb (
		id      INTEGER PRIMARY KEY CHECK(id = 1),
		data    BLOB NOT NULL,
		updated TEXT NOT NULL
	);`,

	// _vibe_field_meta stores Access-style field properties (plan §1.3):
	// human captions, control types, defaults, validation rules, input masks,
	// and lookup sources — keyed by (table_name, column_name). Additive migration.
	`CREATE TABLE IF NOT EXISTS _vibe_field_meta (
		table_name         TEXT NOT NULL,
		column_name        TEXT NOT NULL,
		caption            TEXT NOT NULL DEFAULT '',
		control_type       TEXT NOT NULL DEFAULT '',
		default_value      TEXT NOT NULL DEFAULT '',
		required           INTEGER NOT NULL DEFAULT 0,
		format_code        TEXT NOT NULL DEFAULT '',
		validation_rule    TEXT NOT NULL DEFAULT '',
		validation_message TEXT NOT NULL DEFAULT '',
		input_mask         TEXT NOT NULL DEFAULT '',
		lookup_sql         TEXT NOT NULL DEFAULT '',
		display_col        TEXT NOT NULL DEFAULT '',
		value_col          TEXT NOT NULL DEFAULT '',
		control_tip        TEXT NOT NULL DEFAULT '',
		sort_order         INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (table_name, column_name)
	);`,

	// _vibe_forms stores Access-style form definitions (plan §1.5/§1.6) as
	// versioned JSON: a form binds a table to a set of bound controls and an
	// archetype (columnar | continuous | datasheet | master_detail); a
	// master_detail form additionally carries an FK-linked subform. Additive
	// migration.
	`CREATE TABLE IF NOT EXISTS _vibe_forms (
		id         TEXT PRIMARY KEY,
		title      TEXT NOT NULL DEFAULT '',
		table_name TEXT NOT NULL DEFAULT '',
		archetype  TEXT NOT NULL DEFAULT '',
		version    INTEGER NOT NULL DEFAULT 1,
		spec       TEXT NOT NULL,
		created_at TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT ''
	);`,

	// _vibe_relationships stores Access-style foreign-key relationship
	// definitions (plan §2.1/§4.5): child.<child_column> references
	// parent.<parent_column>, with referential-action and enforce-integrity
	// flags. This is the metadata source of truth that the relationship diagram
	// and FK-emit logic (P2.3) read from. Additive migration.
	`CREATE TABLE IF NOT EXISTS _vibe_relationships (
		id                TEXT PRIMARY KEY,
		parent_table      TEXT NOT NULL DEFAULT '',
		parent_column     TEXT NOT NULL DEFAULT '',
		child_table       TEXT NOT NULL DEFAULT '',
		child_column      TEXT NOT NULL DEFAULT '',
		on_delete         TEXT NOT NULL DEFAULT 'NO ACTION',
		on_update         TEXT NOT NULL DEFAULT 'NO ACTION',
		enforce_integrity INTEGER NOT NULL DEFAULT 1,
		created_at        TEXT NOT NULL DEFAULT ''
	);`,

	// _vibe_reports stores Access-style banded report definitions (plan §3.2 /
	// P3.7) as versioned JSON. A report binds a SQL source to grouping levels
	// (group header/footer bands), a detail-row sort, detail columns, and
	// group-footer / report-footer aggregate expressions. The grouping/sort/band
	// portions are split into dedicated JSON columns so they can be queried or
	// migrated independently. Additive migration.
	`CREATE TABLE IF NOT EXISTS _vibe_reports (
		id         TEXT PRIMARY KEY,
		title      TEXT NOT NULL DEFAULT '',
		source     TEXT NOT NULL DEFAULT '',
		groups     TEXT NOT NULL DEFAULT '[]',
		sort       TEXT NOT NULL DEFAULT '[]',
		bands      TEXT NOT NULL DEFAULT '{}',
		version    INTEGER NOT NULL DEFAULT 1,
		created_at TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT ''
	);`,

	// _vibe_nav stores Access-style Switchboard navigation (plan §4.2/§4.5). Each
	// row is a single switchboard menu item belonging to a switchboard "page"
	// (parent_id nesting for drill-down), carrying a human label, an action kind
	// (open_form | open_report | open_page | open_switchboard | run_macro), a
	// target id, and a sort order. The App Shell + switchboard generator read this
	// store to render the locked-down navigation. Additive migration.
	`CREATE TABLE IF NOT EXISTS _vibe_nav (
		id          TEXT PRIMARY KEY,
		parent_id   TEXT NOT NULL DEFAULT '',
		label       TEXT NOT NULL DEFAULT '',
		action_kind TEXT NOT NULL DEFAULT '',
		target_id   TEXT NOT NULL DEFAULT '',
		sort_order  INTEGER NOT NULL DEFAULT 0
	);`,

	// _vibe_macros stores Access-style declarative macros (plan §5.1) as
	// versioned JSON. A macro is an ordered list of actions (OpenForm,
	// OpenReport, RunSQL, MsgBox, ...), each optionally guarded by an If/Then
	// condition. The declarative action engine + command-button control (P5.2)
	// read this store. Additive migration.
	`CREATE TABLE IF NOT EXISTS _vibe_macros (
		id         TEXT PRIMARY KEY,
		title      TEXT NOT NULL DEFAULT '',
		version    INTEGER NOT NULL DEFAULT 1,
		spec       TEXT NOT NULL,
		created_at TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT ''
	);`,
}

// defaultMeta contains the default metadata entries seeded on creation.
var defaultMeta = map[string]string{
	"schema_version": currentSchemaVersion,
	"format_version": "1.0",
	"created_at":     "", // set at runtime
	"updated_at":     "", // set at runtime
	"title":          "",
	"description":    "",
	"agent_sandbox":  "read-only",
	// default_mode is the persisted startup mode for the app shell (Phase 4
	// dual-mode). New files default to "studio"; data-entry deployments may
	// flip it to "app" via SetAppConfig.
	"default_mode": "studio",
	// LLM endpoint config for the in-app conversational agent (W0). The agent
	// loop (internal/llm) talks to any OpenAI-compatible server; the default
	// targets a local llama-server. Read with a fallback so files created
	// before these keys existed still work.
	"llm_endpoint": "http://localhost:8080/v1",
	"llm_model":    "local",
	"llm_api_key":  "",
}

// runMigrations creates all _vibe_* tables and seeds default metadata.
func runMigrations(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin migration transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	for _, ddl := range schemaDDL {
		if _, err := tx.Exec(ddl); err != nil {
			return fmt.Errorf("failed to execute DDL: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit migration transaction: %w", err)
	}

	// Seed app-shell startup metadata (idempotent) so existing files migrated
	// forward also gain default_mode (Phase 4 dual-mode). seedMeta only runs on
	// Create; this INSERT OR IGNORE backfills opened files.
	if _, err := db.Exec(
		"INSERT OR IGNORE INTO _vibe_meta (key, value) VALUES ('default_mode', 'studio');",
	); err != nil {
		return fmt.Errorf("failed to seed default_mode meta: %w", err)
	}

	// Seed system pages (idempotent) so the Studio Home / Dashboard can render
	// through the page pipeline rather than a hardcoded binding (P3.10).
	if err := seedSystemPages(db); err != nil {
		return fmt.Errorf("failed to seed system pages: %w", err)
	}

	return nil
}

// DashboardPageID is the stable id of the `_dashboard` system page that backs
// the Studio Home / Dashboard screen. DashboardView renders this page through
// the same page pipeline used by the page editor/preview (P3.10), so the home
// screen is data-driven (live metric/table directives) instead of carrying
// hardcoded stats.
const DashboardPageID = "_dashboard"

// dashboardPageContent is the markdown body of the `_dashboard` system page. It
// uses the standard page directives ({metric:}/{table:}) so it executes through
// the normal SQL-block + directive pipeline. The counts query the DuckDB
// information_schema so the dashboard reflects live state.
const dashboardPageContent = "# Dashboard\n\n" +
	"Welcome to your Vibe workspace. The numbers below are computed live through the page pipeline.\n\n" +
	"```sql counts\n" +
	"SELECT\n" +
	"  (SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'main') AS tables\n" +
	"```\n\n" +
	"{metric: source=counts, value=tables, label=Tables}\n\n" +
	"## Tables in this workspace\n\n" +
	"```sql table_list\n" +
	"SELECT table_name AS \"Table\" FROM information_schema.tables WHERE table_schema = 'main' ORDER BY table_name\n" +
	"```\n\n" +
	"{table: source=table_list}\n"

// seedSystemPages inserts (idempotently) the built-in system pages, currently
// just the `_dashboard` page. Uses INSERT OR IGNORE so user edits to an existing
// `_dashboard` page are never clobbered and re-running migrations on an existing
// container is a no-op.
func seedSystemPages(db *sql.DB) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`INSERT OR IGNORE INTO _vibe_pages
			(id, title, content, page_type, sort_order, created_at, updated_at, metadata)
		 VALUES (?, ?, ?, 'notebook', -100, ?, ?, '{"system":true}');`,
		DashboardPageID, "Dashboard", dashboardPageContent, now, now,
	)
	if err != nil {
		return fmt.Errorf("failed to seed %q page: %w", DashboardPageID, err)
	}
	return nil
}

// seedMeta inserts default metadata entries if they do not already exist.
func seedMeta(db *sql.DB, title, description string) error {
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin seed transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	for key, value := range defaultMeta {
		switch key {
		case "created_at", "updated_at":
			value = now
		case "title":
			if title != "" {
				value = title
			}
		case "description":
			if description != "" {
				value = description
			}
		}

		_, err := tx.Exec(
			"INSERT OR IGNORE INTO _vibe_meta (key, value) VALUES (?, ?);",
			key, value,
		)
		if err != nil {
			return fmt.Errorf("failed to seed meta key %q: %w", key, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit seed transaction: %w", err)
	}

	return nil
}

// getSchemaVersion returns the current schema version from _vibe_meta, or empty string if not set.
func getSchemaVersion(db *sql.DB) (string, error) {
	var version string
	err := db.QueryRow("SELECT value FROM _vibe_meta WHERE key = 'schema_version';").Scan(&version)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to read schema version: %w", err)
	}
	return version, nil
}
