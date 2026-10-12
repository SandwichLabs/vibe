package prefs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestRecents(t *testing.T) (*Recents, *Store) {
	t.Helper()
	s := NewStore(t.TempDir())
	return NewRecents(s, nil), s
}

func TestRecents_RecordOrdersMostRecentFirstAndCaps(t *testing.T) {
	r, _ := newTestRecents(t)
	for i := 0; i < MaxRecents+3; i++ {
		r.Record(fmt.Sprintf("/w/%02d", i), fmt.Sprintf("id%d", i), fmt.Sprintf("W%d", i))
	}
	list := r.List()
	require.Len(t, list, MaxRecents)
	assert.Equal(t, fmt.Sprintf("/w/%02d", MaxRecents+2), list[0].Path, "the newest is first")
	// Recording an old one again moves it to the top and renames it.
	r.Record(list[3].Path, "newid", "Renamed")
	list = r.List()
	assert.Equal(t, "Renamed", list[0].Name)
	assert.Equal(t, "newid", list[0].ID)
	assert.Len(t, list, MaxRecents)
}

func TestRecents_RemoveRestoreRelocate(t *testing.T) {
	r, _ := newTestRecents(t)
	a, b := t.TempDir(), t.TempDir()
	r.Record(a, "ida", "A")
	r.Record(b, "idb", "B")

	removed, err := r.Remove(a)
	require.NoError(t, err)
	require.NotNil(t, removed)
	assert.Equal(t, "ida", removed.ID)
	assert.Len(t, r.List(), 1)
	none, err := r.Remove(filepath.Join(a, "nope"))
	require.NoError(t, err)
	assert.Nil(t, none, "an unlisted path removes nothing")

	require.NoError(t, r.Restore(*removed))
	require.NoError(t, r.Restore(*removed), "restoring a listed path is a no-op")
	assert.Len(t, r.List(), 2)
	require.Error(t, r.Restore(Recent{Path: "  "}))

	moved := t.TempDir()
	require.NoError(t, r.Relocate(a, moved))
	paths := []string{}
	for _, e := range r.List() {
		paths = append(paths, e.Path)
	}
	assert.Contains(t, paths, moved)
	assert.NotContains(t, paths, a)
	assert.Equal(t, map[string]string{moved: "ida", b: "idb"}, r.IDs())
}

func TestRecents_SamePathResolvesSymlinks(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(dir, link))
	assert.True(t, SamePath(dir, link))
	assert.True(t, SamePath("/x/y", "/x/y"))
	assert.False(t, SamePath("/x/y", "/x/z"))
}

func TestRecents_SavePreferencesKeepsTheListFromDisk(t *testing.T) {
	r, s := newTestRecents(t)
	r.Record("/w/one", "1", "One")
	stale, err := s.Load()
	require.NoError(t, err)
	r.Record("/w/two", "2", "Two")

	stale.Theme = "schoolhouse-dark"
	stale.Extra["other"] = json.RawMessage(`"kept"`)
	require.NoError(t, r.SavePreferences(stale))
	assert.Len(t, r.List(), 2, "a stale copy must not roll the list back")
	got, err := s.Load()
	require.NoError(t, err)
	assert.Equal(t, "schoolhouse-dark", got.Theme)
	assert.JSONEq(t, `"kept"`, string(got.Extra["other"]))

	// A copy that carries no list also keeps the one on disk.
	require.NoError(t, r.SavePreferences(Defaults()))
	assert.Len(t, r.List(), 2)
}

func TestRecents_NilStoreIsInert(t *testing.T) {
	r := NewRecents(nil, nil)
	r.Record("/w", "id", "W")
	assert.Empty(t, r.List())
	removed, err := r.Remove("/w")
	require.NoError(t, err)
	assert.Nil(t, removed)
	require.NoError(t, r.Restore(Recent{Path: "/w"}))
	assert.Error(t, r.SavePreferences(Defaults()))
	var none *Recents
	none.Record("/w", "id", "W")
	assert.Empty(t, none.List())
}

func TestRecents_UnreadableListStartsOver(t *testing.T) {
	r, s := newTestRecents(t)
	p := Defaults()
	p.Extra = map[string]json.RawMessage{RecentsKey: json.RawMessage(`"not a list"`)}
	require.NoError(t, s.Save(p))
	assert.Empty(t, r.List())
	r.Record("/w", "id", "W")
	assert.Len(t, r.List(), 1)
}
