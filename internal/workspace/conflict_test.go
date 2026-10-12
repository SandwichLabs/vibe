package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/filestore"
	"github.com/SandwichLabs/vibe/internal/queries"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/skills"
)

func conflictFixture(t *testing.T) (Workspace, session.Session) {
	t.Helper()
	ws, _ := newWorkspace(t)
	sess, _, err := OpenSession(context.Background(), ws, nil, WithSessionOptions(session.WithExtensionDirectory(t.TempDir())))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sess.Close() })
	return ws, sess
}

func appendToFile(t *testing.T, path, text string) {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(b, []byte(text)...), 0o600))
}

func TestResolveConflict_RejectsUnknownActionAndKindBeforeTouchingAnything(t *testing.T) {
	ws, sess := conflictFixture(t)
	_, err := ResolveConflict(context.Background(), ws, sess, ConflictRequest{Kind: ConflictNotebook, ID: "x", Action: "merge"})
	assert.EqualError(t, err, `unknown action "merge"`)
	_, err = ResolveConflict(context.Background(), ws, sess, ConflictRequest{Kind: "dashboard", ID: "x", Action: ConflictReload})
	assert.EqualError(t, err, `unknown kind "dashboard"`)
}

func TestResolveConflict_ReloadOfAMissingItemIsNotAConflict(t *testing.T) {
	ws, sess := conflictFixture(t)
	for _, kind := range []string{ConflictNotebook, ConflictQuery, ConflictSkill} {
		_, err := ResolveConflict(context.Background(), ws, sess, ConflictRequest{Kind: kind, ID: "missing-one", Action: ConflictReload})
		require.Error(t, err, kind)
		assert.NotErrorIs(t, err, filestore.ErrConflict, kind)
	}
}

func TestResolveConflict_Notebook(t *testing.T) {
	ws, sess := conflictFixture(t)
	ctx := context.Background()
	nb, _, err := ws.Notebooks().Save(ctx, map[string]any{"title": "Weekly", "cells": []any{}})
	require.NoError(t, err)
	path := filepath.Join(ws.Root(), "notebooks", nb.ID+".vnb")
	local := map[string]any{"id": nb.ID, "rev": nb.Rev, "title": "My edit", "cells": []any{}}

	// Someone else saves the file.
	appendToFile(t, path, "\n")
	disk, _, err := ws.Notebooks().Get(ctx, nb.ID)
	require.NoError(t, err)

	t.Run("reload reports the disk revision", func(t *testing.T) {
		res, err := ResolveConflict(ctx, ws, sess, ConflictRequest{Kind: ConflictNotebook, ID: nb.ID, Action: ConflictReload})
		require.NoError(t, err)
		assert.Equal(t, &ConflictResult{ID: nb.ID, Rev: disk.Rev, Title: "Weekly"}, res)
	})
	t.Run("saveCopy drops id and rev, titles the copy and does not alter the caller's map", func(t *testing.T) {
		res, err := ResolveConflict(ctx, ws, sess, ConflictRequest{Kind: ConflictNotebook, ID: nb.ID, Action: ConflictSaveCopy, Data: local})
		require.NoError(t, err)
		assert.NotEqual(t, nb.ID, res.ID)
		assert.Equal(t, "My edit (copy)", res.Title)
		assert.Equal(t, nb.ID, local["id"])
		assert.Equal(t, nb.Rev, local["rev"])
	})
	t.Run("a blank title becomes Untitled (copy)", func(t *testing.T) {
		res, err := ResolveConflict(ctx, ws, sess, ConflictRequest{Kind: ConflictNotebook, ID: nb.ID, Action: ConflictSaveCopy,
			Data: map[string]any{"title": "  ", "cells": []any{}}})
		require.NoError(t, err)
		assert.Equal(t, "Untitled (copy)", res.Title)
	})
	t.Run("overwrite with a stale banner revision is refused and writes nothing", func(t *testing.T) {
		_, err := ResolveConflict(ctx, ws, sess, ConflictRequest{Kind: ConflictNotebook, ID: nb.ID, Action: ConflictOverwrite, Rev: nb.Rev, Data: local})
		require.ErrorIs(t, err, filestore.ErrConflict)
		got, _, err := ws.Notebooks().Get(ctx, nb.ID)
		require.NoError(t, err)
		assert.Equal(t, "Weekly", got.Title)
	})
	t.Run("overwrite with the banner's revision keeps mine", func(t *testing.T) {
		res, err := ResolveConflict(ctx, ws, sess, ConflictRequest{Kind: ConflictNotebook, ID: nb.ID, Action: ConflictOverwrite, Rev: disk.Rev, Data: local})
		require.NoError(t, err)
		assert.Equal(t, "My edit", res.Title)
		assert.Equal(t, nb.ID, res.ID, "the id on the request wins over whatever the document says")
	})
	t.Run("overwrite without a revision, after a delete on disk, writes it back", func(t *testing.T) {
		require.NoError(t, os.Remove(path))
		res, err := ResolveConflict(ctx, ws, sess, ConflictRequest{Kind: ConflictNotebook, ID: nb.ID, Action: ConflictOverwrite,
			Data: map[string]any{"title": "Back", "cells": []any{}}})
		require.NoError(t, err)
		assert.Equal(t, nb.ID, res.ID)
		assert.FileExists(t, path)
	})
}

func TestResolveConflict_Query(t *testing.T) {
	ws, sess := conflictFixture(t)
	ctx := context.Background()
	store := ws.Queries()
	require.NoError(t, os.WriteFile(filepath.Join(ws.Root(), "queries", "daily.sql"),
		[]byte("-- vibe: {\"title\":\"Daily\",\"description\":\"by the agent\",\"connection\":\"local\"}\nSELECT 1\n"), 0o600))
	base, err := store.Get(ctx, "daily")
	require.NoError(t, err)
	mine := map[string]any{"title": "Daily", "sql": "SELECT 2"}

	t.Run("reload", func(t *testing.T) {
		res, err := ResolveConflict(ctx, ws, sess, ConflictRequest{Kind: ConflictQuery, ID: "daily", Action: ConflictReload})
		require.NoError(t, err)
		assert.Equal(t, &ConflictResult{ID: "daily", Rev: base.Rev, Title: "Daily"}, res)
	})
	t.Run("keep mine refuses a file that changed again, and writes nothing", func(t *testing.T) {
		appendToFile(t, filepath.Join(ws.Root(), "queries", "daily.sql"), "-- again\n")
		_, err := ResolveConflict(ctx, ws, sess, ConflictRequest{Kind: ConflictQuery, ID: "daily", Action: ConflictOverwrite, Rev: base.Rev, Data: mine})
		require.ErrorIs(t, err, filestore.ErrConflict)
		got, err := store.Get(ctx, "daily")
		require.NoError(t, err)
		assert.Contains(t, got.SQL, "SELECT 1")
	})
	t.Run("keep mine keeps what the editor does not edit, and its description wins", func(t *testing.T) {
		_, err := ResolveConflict(ctx, ws, sess, ConflictRequest{Kind: ConflictQuery, ID: "daily", Action: ConflictOverwrite, Data: mine})
		require.NoError(t, err)
		got, err := store.Get(ctx, "daily")
		require.NoError(t, err)
		assert.Equal(t, "SELECT 2", got.SQL)
		assert.Equal(t, "by the agent", got.Description)
		assert.Equal(t, "local", got.Connection)
		mine["description"] = "mine"
		_, err = ResolveConflict(ctx, ws, sess, ConflictRequest{Kind: ConflictQuery, ID: "daily", Action: ConflictOverwrite, Data: mine})
		require.NoError(t, err)
		got, _ = store.Get(ctx, "daily")
		assert.Equal(t, "mine", got.Description)
	})
	t.Run("the editor's Run on choice wins; empty is the default; a bad name is refused", func(t *testing.T) {
		other, empty, bad := "other", "", `x"; DROP`
		_, err := ResolveConflict(ctx, ws, sess, ConflictRequest{Kind: ConflictQuery, ID: "daily", Action: ConflictOverwrite, Connection: &other, Data: mine})
		require.NoError(t, err)
		got, _ := store.Get(ctx, "daily")
		assert.Equal(t, "other", got.Connection)
		cp, err := ResolveConflict(ctx, ws, sess, ConflictRequest{Kind: ConflictQuery, ID: "daily", Action: ConflictSaveCopy, Connection: &empty, Data: mine})
		require.NoError(t, err)
		c, err := store.Get(ctx, cp.ID)
		require.NoError(t, err)
		assert.Empty(t, c.Connection)
		assert.Equal(t, "Daily (copy)", cp.Title)
		_, err = ResolveConflict(ctx, ws, sess, ConflictRequest{Kind: ConflictQuery, ID: "daily", Action: ConflictOverwrite, Connection: &bad, Data: mine})
		assert.EqualError(t, err, `unknown connection "x\"; DROP"`)
	})
	t.Run("keep mine after a delete on disk writes it back, as a new id", func(t *testing.T) {
		q := &queries.Query{Title: "Gone", SQL: "SELECT 1", Engine: "duckdb", Parameters: []queries.Param{}}
		require.NoError(t, store.Create(ctx, q))
		require.NoError(t, os.Remove(filepath.Join(ws.Root(), "queries", q.ID+".sql")))
		for _, rev := range []string{"", q.Rev} {
			res, err := ResolveConflict(ctx, ws, sess, ConflictRequest{Kind: ConflictQuery, ID: q.ID, Action: ConflictOverwrite, Rev: rev,
				Data: map[string]any{"title": "Gone", "sql": "SELECT 3"}})
			require.NoError(t, err, "rev %q", rev)
			got, err := store.Get(ctx, res.ID)
			require.NoError(t, err)
			assert.Equal(t, "SELECT 3", got.SQL)
			require.NoError(t, os.Remove(filepath.Join(ws.Root(), "queries", res.ID+".sql")))
		}
	})
}

func TestResolveConflict_Skill(t *testing.T) {
	ws, sess := conflictFixture(t)
	ctx := context.Background()
	store := ws.Skills()
	_, err := skills.SaveUserSkillAt(store, sess, "rules", "desc", "original", "", "")
	require.NoError(t, err)
	spath := filepath.Join(ws.Root(), "skills", "rules", "SKILL.md")
	diskRev := func() string {
		cur, err := store.Get(ctx, "rules")
		require.NoError(t, err)
		return cur.Rev
	}
	mine := map[string]any{"description": "desc", "instructions": "mine"}

	appendToFile(t, spath, "\nfirst outside edit\n")
	banner := diskRev()
	appendToFile(t, spath, "\nsecond outside edit\n")

	t.Run("reload reports the disk revision under the skill's name", func(t *testing.T) {
		res, err := ResolveConflict(ctx, ws, sess, ConflictRequest{Kind: ConflictSkill, ID: "rules", Action: ConflictReload})
		require.NoError(t, err)
		assert.Equal(t, &ConflictResult{ID: "rules", Rev: diskRev(), Title: "rules"}, res)
	})
	t.Run("keep mine with a stale banner revision is refused", func(t *testing.T) {
		_, err := ResolveConflict(ctx, ws, sess, ConflictRequest{Kind: ConflictSkill, ID: "rules", Action: ConflictOverwrite, Rev: banner, Data: mine})
		require.ErrorIs(t, err, filestore.ErrConflict)
		b, _ := os.ReadFile(spath)
		assert.Contains(t, string(b), "second outside edit")
	})
	t.Run("keep mine with the banner's revision goes through", func(t *testing.T) {
		res, err := ResolveConflict(ctx, ws, sess, ConflictRequest{Kind: ConflictSkill, ID: "rules", Action: ConflictOverwrite, Rev: diskRev(), Data: mine})
		require.NoError(t, err)
		assert.Equal(t, "rules", res.ID)
		assert.NotEmpty(t, res.Rev)
		b, _ := os.ReadFile(spath)
		assert.Contains(t, string(b), "mine")
	})
	t.Run("keep mine without a revision replaces whatever is on disk now", func(t *testing.T) {
		appendToFile(t, spath, "\nsomeone else\n")
		_, err := ResolveConflict(ctx, ws, sess, ConflictRequest{Kind: ConflictSkill, ID: "rules", Action: ConflictOverwrite, Data: mine})
		require.NoError(t, err)
		b, _ := os.ReadFile(spath)
		assert.NotContains(t, string(b), "someone else")
	})
	t.Run("save as copy never overwrites an earlier copy", func(t *testing.T) {
		first, err := ResolveConflict(ctx, ws, sess, ConflictRequest{Kind: ConflictSkill, ID: "rules", Action: ConflictSaveCopy, Data: mine})
		require.NoError(t, err)
		second, err := ResolveConflict(ctx, ws, sess, ConflictRequest{Kind: ConflictSkill, ID: "rules", Action: ConflictSaveCopy, Data: mine})
		require.NoError(t, err)
		assert.Equal(t, "rules-copy", first.ID)
		assert.Equal(t, "rules-copy-2", second.ID)
		assert.Equal(t, second.ID, second.Title)
	})
	t.Run("keep mine after a delete on disk writes it back", func(t *testing.T) {
		require.NoError(t, os.RemoveAll(filepath.Dir(spath)))
		_, err := ResolveConflict(ctx, ws, sess, ConflictRequest{Kind: ConflictSkill, ID: "rules", Action: ConflictOverwrite, Data: mine})
		require.NoError(t, err)
		assert.FileExists(t, spath)
	})
	t.Run("reload of a deleted skill is not-found", func(t *testing.T) {
		require.NoError(t, os.RemoveAll(filepath.Dir(spath)))
		_, err := ResolveConflict(ctx, ws, sess, ConflictRequest{Kind: ConflictSkill, ID: "rules", Action: ConflictReload})
		assert.ErrorIs(t, err, skills.ErrNotFound)
	})
}
