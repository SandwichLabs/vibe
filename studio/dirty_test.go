package main

// Dirty-tracking tests (#81): a single flag on the open container tracks
// whether DuckDB user data has changed since the last Save, driving the
// sidebar indicator and the Close/quit prompt. The safety rule these tests
// exist to prove is that CloseFile still saves and CloseFileWithoutSaving
// is the only way to discard a table change.

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/agents"
	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirty_InsertMarksAndSaveClears(t *testing.T) {
	app := newOpenApp(t)
	require.NoError(t, app.CreateTable("widgets", []ColumnInfo{{Name: "id", Type: "INTEGER", PK: true}}))
	require.NoError(t, app.SaveFile())
	require.False(t, app.IsDirty())

	var events []any
	app.emit = func(name string, data ...any) {
		if name == "file:dirty" {
			events = append(events, data[0])
		}
	}

	_, err := app.InsertRow("widgets", map[string]any{"id": 1})
	require.NoError(t, err)
	assert.True(t, app.IsDirty())
	require.Len(t, events, 1, "exactly one file:dirty event for the clean->dirty edge")
	assert.Equal(t, true, events[0])

	// A second write while already dirty must not emit again.
	_, err = app.InsertRow("widgets", map[string]any{"id": 2})
	require.NoError(t, err)
	require.Len(t, events, 1)

	require.NoError(t, app.SaveFile())
	assert.False(t, app.IsDirty())
	require.Len(t, events, 2)
	assert.Equal(t, false, events[1])
}

// TestDirty_SaveFileEmitsActualDirtyState (review round 2, required item 2):
// SaveFile must emit file:dirty with the container's ACTUAL Dirty() state
// after the checkpoint, not a hard-coded false — a write landing while a
// concurrent Save is in flight (an agent, MCP, or another RLock holder;
// SaveFile only takes RLock, same as ExecuteQuery and agentToolHandler.
// Handle, so they can run truly concurrently with a Save) must never leave
// the indicator stuck on "Saved" while the file is dirty again. Races
// ExecuteQuery writes against SaveFile repeatedly and asserts the LAST
// emitted file:dirty value always matches IsDirty() once everything has
// settled.
func TestDirty_SaveFileEmitsActualDirtyState(t *testing.T) {
	app := newOpenApp(t)
	require.NoError(t, app.CreateTable("widgets", []ColumnInfo{{Name: "id", Type: "INTEGER", PK: true}}))
	require.NoError(t, app.SaveFile())

	var mu sync.Mutex
	var lastDirtyEvent bool
	app.emit = func(name string, data ...any) {
		if name != "file:dirty" || len(data) == 0 {
			return
		}
		mu.Lock()
		lastDirtyEvent, _ = data[0].(bool)
		mu.Unlock()
	}

	for i := 0; i < 50; i++ {
		var wg sync.WaitGroup
		wg.Add(2)
		go func(n int) {
			defer wg.Done()
			_, _ = app.ExecuteQuery(fmt.Sprintf("INSERT INTO widgets VALUES (%d)", n))
		}(i)
		go func() {
			defer wg.Done()
			_ = app.SaveFile()
		}()
		wg.Wait()

		mu.Lock()
		got := lastDirtyEvent
		mu.Unlock()
		assert.Equal(t, app.IsDirty(), got, "round %d: last emitted file:dirty must match the container's actual Dirty() once settled", i)
	}
}

func TestDirty_ExecuteQueryClassifiesReadsAndWrites(t *testing.T) {
	app := newOpenApp(t)

	_, err := app.ExecuteQuery("SELECT 1")
	require.NoError(t, err)
	assert.False(t, app.IsDirty(), "a plain SELECT must not mark the file dirty")

	_, err = app.ExecuteQuery("CREATE TABLE z (a INTEGER)")
	require.NoError(t, err)
	assert.True(t, app.IsDirty(), "a DDL write must mark the file dirty")

	require.NoError(t, app.SaveFile())
	assert.False(t, app.IsDirty())

	// A failing query changes nothing, even when the classifier can't
	// resolve it and treats it as a write.
	_, err = app.ExecuteQuery("SELECT * FROM nonexistent_table")
	assert.Error(t, err)
	assert.False(t, app.IsDirty(), "a failed query must not mark the file dirty")
}

func TestDirty_CreateTableAddColumnImport(t *testing.T) {
	t.Run("CreateTable", func(t *testing.T) {
		app := newOpenApp(t)
		require.NoError(t, app.CreateTable("t1", []ColumnInfo{{Name: "id", Type: "INTEGER", PK: true}}))
		assert.True(t, app.IsDirty())
	})

	t.Run("AddColumn", func(t *testing.T) {
		app := newOpenApp(t)
		require.NoError(t, app.CreateTable("t2", []ColumnInfo{{Name: "id", Type: "INTEGER", PK: true}}))
		require.NoError(t, app.SaveFile())
		require.False(t, app.IsDirty())
		require.NoError(t, app.AddColumn("t2", "name", "VARCHAR"))
		assert.True(t, app.IsDirty())
	})
}

// TestDirty_AgentWritePaths covers the run_query classification and a
// content-category tool call (add_query_cell), through the exact dispatch
// path StartMCPServer/Studio chat share.
func TestDirty_AgentWritePaths(t *testing.T) {
	app := newOpenApp(t)
	require.NoError(t, app.SetAgentConfig(AgentConfig{Sandbox: "full", AskBeforeWrites: false}))
	require.NoError(t, app.CreateTable("widgets", []ColumnInfo{{Name: "id", Type: "INTEGER", PK: true}}))
	require.NoError(t, app.SaveFile())
	require.False(t, app.IsDirty())

	app.mu.RLock()
	handler := app.newAgentToolHandler(agents.ModeFull)
	app.mu.RUnlock()

	ctx := context.Background()

	selectResp := handler.Handle(ctx, &agents.Request{JSONRPC: "2.0", ID: 1, Method: "run_query",
		Params: map[string]any{"sql": "SELECT * FROM widgets"}})
	require.Nil(t, selectResp.Error)
	assert.False(t, app.IsDirty(), "a run_query SELECT must not mark dirty")

	insertResp := handler.Handle(ctx, &agents.Request{JSONRPC: "2.0", ID: 2, Method: "run_query",
		Params: map[string]any{"sql": "INSERT INTO widgets VALUES (1)"}})
	require.Nil(t, insertResp.Error)
	assert.True(t, app.IsDirty(), "a run_query INSERT must mark dirty")

	require.NoError(t, app.SaveFile())
	require.False(t, app.IsDirty())

	cellResp := handler.Handle(ctx, &agents.Request{JSONRPC: "2.0", ID: 3, Method: "add_query_cell",
		Params: map[string]any{"title": "t", "sql": "SELECT 1"}})
	require.Nil(t, cellResp.Error)
	assert.False(t, app.IsDirty(), "add_query_cell (content category) writes SQLite only and must not mark dirty")
}

// TestCloseFileWithoutSaving_DiscardsTableChanges is the acceptance case:
// "Don't save" discards a table edit on reopen, while CloseFile (Save)
// keeps it. Neither ever skips releasing the #80 writer lock.
func TestCloseFileWithoutSaving_DiscardsTableChanges(t *testing.T) {
	app := newOpenApp(t)
	require.NoError(t, app.CreateTable("widgets", []ColumnInfo{{Name: "id", Type: "INTEGER", PK: true}}))
	require.NoError(t, app.SaveFile())

	_, err := app.InsertRow("widgets", map[string]any{"id": 1})
	require.NoError(t, err)
	require.True(t, app.IsDirty())

	path := app.filePath
	require.NoError(t, app.CloseFileWithoutSaving())

	c, err := container.Open(path, container.WithPassphrase(testPassphrase), container.WithLogger(testLogger()))
	require.NoError(t, err, "the writer lock must be released so the file can be reopened")
	defer c.Close() //nolint:errcheck

	var count int
	require.NoError(t, c.DuckDBDB().QueryRow(`SELECT COUNT(*) FROM widgets`).Scan(&count))
	assert.Equal(t, 0, count, "Don't save must discard the row inserted since the last Save")
}

func TestCloseFile_SavesTableChanges(t *testing.T) {
	app := newOpenApp(t)
	require.NoError(t, app.CreateTable("widgets", []ColumnInfo{{Name: "id", Type: "INTEGER", PK: true}}))
	require.NoError(t, app.SaveFile())

	_, err := app.InsertRow("widgets", map[string]any{"id": 1})
	require.NoError(t, err)

	path := app.filePath
	require.NoError(t, app.CloseFile())

	c, err := container.Open(path, container.WithPassphrase(testPassphrase), container.WithLogger(testLogger()))
	require.NoError(t, err)
	defer c.Close() //nolint:errcheck

	var count int
	require.NoError(t, c.DuckDBDB().QueryRow(`SELECT COUNT(*) FROM widgets`).Scan(&count))
	assert.Equal(t, 1, count, "CloseFile must save the row inserted since the last Save")
}

// TestCloseFileWithoutSaving_SQLiteContentSurvives documents the model: a
// notebook (SQLite-side, write-through) created before CloseFileWithoutSaving
// survives the reopen even though the table edit didn't.
func TestCloseFileWithoutSaving_SQLiteContentSurvives(t *testing.T) {
	app := newOpenApp(t)
	nb, err := app.CreateNotebook("Kept Notebook")
	require.NoError(t, err)

	path := app.filePath
	require.NoError(t, app.CloseFileWithoutSaving())

	c, err := container.Open(path, container.WithPassphrase(testPassphrase), container.WithLogger(testLogger()))
	require.NoError(t, err)
	defer c.Close() //nolint:errcheck

	var title string
	require.NoError(t, c.SQLiteDB().QueryRow(`SELECT title FROM _vibe_notebooks WHERE id = ?`, nb.ID).Scan(&title))
	assert.Equal(t, "Kept Notebook", title)
}
