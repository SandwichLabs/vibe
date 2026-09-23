package notebooks

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMigrateOnOpen_RunsPageMigration is the shared-entry-point regression
// guard: before this helper existed, only Studio's OpenFile/CreateFile
// called MigratePages/MigrateReports — a legacy file opened via
// `vibe mcp serve` or the TUI kept its pages invisible. MigrateOnOpen is the
// one function every entry point now calls.
func TestMigrateOnOpen_RunsPageMigration(t *testing.T) {
	db := migrateTestDB(t)
	insertPage(t, db, "welcome", "Welcome to Vibe", welcomePage, "markdown")

	MigrateOnOpen(db, false, nil)

	list, err := List(db)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "Welcome to Vibe", list[0].Title)

	// Idempotent: calling it again must not duplicate the notebook.
	MigrateOnOpen(db, false, nil)
	list, err = List(db)
	require.NoError(t, err)
	assert.Len(t, list, 1)
}

// TestMigrateOnOpen_ReadOnlySkipsMigration asserts a read-only open never
// writes to the database at all — see MigrateOnOpen's doc comment for why
// "migrate in memory" isn't safe for the SQLite side of a read-only-opened
// container.
func TestMigrateOnOpen_ReadOnlySkipsMigration(t *testing.T) {
	db := migrateTestDB(t)
	insertPage(t, db, "welcome", "Welcome to Vibe", welcomePage, "markdown")

	MigrateOnOpen(db, true, nil)

	list, err := List(db)
	require.NoError(t, err)
	assert.Empty(t, list, "read-only open must not migrate (or write) anything")

	var migratedTo string
	err = db.QueryRow(`SELECT migrated_to FROM _vibe_pages WHERE id = 'welcome'`).Scan(&migratedTo)
	require.NoError(t, err)
	assert.Empty(t, migratedTo, "the source page row must be untouched under a read-only open")
}

// TestMigrateOnOpen_NilLoggerDoesNotPanic covers the nil-logger convenience
// default (some callers, e.g. tests, may not have one wired up).
func TestMigrateOnOpen_NilLoggerDoesNotPanic(t *testing.T) {
	db := migrateTestDB(t)
	assert.NotPanics(t, func() { MigrateOnOpen(db, false, nil) })
	assert.NotPanics(t, func() { MigrateOnOpen(db, true, nil) })
}
