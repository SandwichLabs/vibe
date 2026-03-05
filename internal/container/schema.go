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
		engine     TEXT NOT NULL DEFAULT 'sqlite',
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
