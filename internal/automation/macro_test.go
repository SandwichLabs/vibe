package automation_test

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/automation"
	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testPassphrase = "test-passphrase-12345"

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func tableExists(t *testing.T, c *container.Container, name string) bool {
	t.Helper()
	var n int
	err := c.SQLiteDB().QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?;", name,
	).Scan(&n)
	require.NoError(t, err)
	return n == 1
}

// sampleSpec builds a two-action macro: an OpenForm followed by an If/Then-guarded
// MsgBox. Used as the canonical round-trip payload.
func sampleSpec() automation.MacroSpec {
	return automation.MacroSpec{
		ID:    "after_save",
		Title: "After Save",
		Actions: []automation.MacroAction{
			{
				Kind: automation.ActionOpenForm,
				Args: map[string]string{
					"form":   "orders_form",
					"filter": "status = 'open'",
				},
			},
			{
				Kind: automation.ActionMsgBox,
				Args: map[string]string{
					"message": "Saved!",
				},
				Condition: &automation.MacroCondition{
					Expr: "[Total] > 100",
				},
			},
		},
	}
}

// TestMacroStoreRoundTrip exercises the full §5.1 acceptance behavior end-to-end
// against a real .vibe container (SQLCipher + DuckDB):
//   - the additive _vibe_macros migration creates the table
//   - Save/Get round-trip a multi-action spec (action kinds, args, condition)
//     byte-faithfully
//   - List returns exactly the stored macros
//   - Replace swaps the action set with no stale actions left behind
//   - Delete removes a macro (Get -> ErrMacroNotFound)
func TestMacroStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "macros.vibe")

	c, err := container.Create(path,
		container.WithPassphrase(testPassphrase),
		container.WithLogger(testLogger()),
		container.WithTitle("Macro Test"),
	)
	require.NoError(t, err)
	defer c.Close() //nolint:errcheck

	// Additive migration must create _vibe_macros in the SQLCipher container.
	require.True(t, tableExists(t, c, "_vibe_macros"),
		"_vibe_macros table should exist after migration")

	store := automation.NewMacroStore(c.SQLiteDB())

	// Save a multi-action spec.
	spec := sampleSpec()
	require.NoError(t, store.Save(spec))

	// Get round-trips byte-faithfully: action kinds, args, and the If/Then
	// condition survive intact.
	got, err := store.Get("after_save")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "after_save", got.ID)
	assert.Equal(t, "After Save", got.Title)
	require.Len(t, got.Actions, 2)

	assert.Equal(t, automation.ActionOpenForm, got.Actions[0].Kind)
	assert.Equal(t, "orders_form", got.Actions[0].Args["form"])
	assert.Equal(t, "status = 'open'", got.Actions[0].Args["filter"])
	assert.Nil(t, got.Actions[0].Condition)

	assert.Equal(t, automation.ActionMsgBox, got.Actions[1].Kind)
	assert.Equal(t, "Saved!", got.Actions[1].Args["message"])
	require.NotNil(t, got.Actions[1].Condition)
	assert.Equal(t, "[Total] > 100", got.Actions[1].Condition.Expr)

	// Spec is versioned so future migrations are possible.
	assert.GreaterOrEqual(t, got.Version, 1, "macro spec should carry a version")

	// List returns exactly the one stored macro.
	all, err := store.List()
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, "after_save", all[0].ID)

	// Replace with a different action set: the old actions must be gone.
	replaced := automation.MacroSpec{
		ID:    "after_save",
		Title: "After Save (v2)",
		Actions: []automation.MacroAction{
			{Kind: automation.ActionRunSQL, Args: map[string]string{"sql": "UPDATE orders SET seen = TRUE"}},
		},
	}
	require.NoError(t, store.Replace(replaced))

	got2, err := store.Get("after_save")
	require.NoError(t, err)
	require.NotNil(t, got2)
	require.Len(t, got2.Actions, 1)
	assert.Equal(t, automation.ActionRunSQL, got2.Actions[0].Kind)
	assert.Equal(t, "UPDATE orders SET seen = TRUE", got2.Actions[0].Args["sql"])

	// Delete then Get returns not-found.
	require.NoError(t, store.Delete("after_save"))
	_, err = store.Get("after_save")
	assert.ErrorIs(t, err, automation.ErrMacroNotFound)
}

// TestMacroMigrationIdempotent asserts that re-running migrations (via reopen)
// is a no-op: the table survives and persisted macros remain.
func TestMacroMigrationIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "macros.vibe")

	c, err := container.Create(path,
		container.WithPassphrase(testPassphrase),
		container.WithLogger(testLogger()),
		container.WithTitle("Macro Test"),
	)
	require.NoError(t, err)

	store := automation.NewMacroStore(c.SQLiteDB())
	require.NoError(t, store.Save(sampleSpec()))

	require.NoError(t, c.Save())
	require.NoError(t, c.Close())

	// Reopen runs migrations again; _vibe_macros must already exist (additive,
	// CREATE TABLE IF NOT EXISTS) and the macro must persist.
	c2, err := container.Open(path,
		container.WithPassphrase(testPassphrase),
		container.WithLogger(testLogger()),
	)
	require.NoError(t, err)
	defer c2.Close() //nolint:errcheck

	require.True(t, tableExists(t, c2, "_vibe_macros"),
		"_vibe_macros should exist after reopen (idempotent migration)")

	store2 := automation.NewMacroStore(c2.SQLiteDB())
	got, err := store2.Get("after_save")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Len(t, got.Actions, 2)
}

// TestMacroValidateRejectsUnknownKind asserts the model rejects an unknown
// action kind, while a spec built from the §5.1 kinds validates clean.
func TestMacroValidateRejectsUnknownKind(t *testing.T) {
	// All §5.1 action-kind constants must be defined and distinct.
	kinds := []string{
		automation.ActionOpenForm,
		automation.ActionOpenReport,
		automation.ActionGoToRecord,
		automation.ActionFindRecord,
		automation.ActionRunQuery,
		automation.ActionRunSQL,
		automation.ActionSetValue,
		automation.ActionSetFilter,
		automation.ActionMsgBox,
		automation.ActionExport,
		automation.ActionEmailObject,
		automation.ActionRunMacro,
	}
	seen := map[string]struct{}{}
	for _, k := range kinds {
		assert.NotEmpty(t, k, "action-kind constant must be non-empty")
		_, dup := seen[k]
		assert.False(t, dup, "action-kind constants must be distinct: %q", k)
		seen[k] = struct{}{}
	}

	// A valid spec passes Validate.
	require.NoError(t, sampleSpec().Validate())

	// An unknown action kind is rejected.
	bad := automation.MacroSpec{
		ID: "bad",
		Actions: []automation.MacroAction{
			{Kind: "Teleport"},
		},
	}
	assert.Error(t, bad.Validate(), "unknown action kind should fail validation")
}
