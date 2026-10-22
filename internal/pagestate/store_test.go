package pagestate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func raw(s string) json.RawMessage { return json.RawMessage(s) }

func open(t *testing.T, root string) *Store {
	t.Helper()
	s, err := Open(root, "pg", WithDelay(time.Hour))
	require.NoError(t, err)
	return s
}

func TestStore_RoundTripAndFileLandsInVibeLocal(t *testing.T) {
	root := t.TempDir()
	s := open(t, root)
	require.NoError(t, s.Set("a", raw(` {"x": [1, 2]} `), nil))
	require.NoError(t, s.Set("b", raw(`"<script>"`), nil))
	v, ok, err := s.Get("a")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.JSONEq(t, `{"x":[1,2]}`, string(v))
	require.NoError(t, s.Flush())
	_, err = os.Stat(filepath.Join(root, ".vibe-local", "artifacts", "pg", "state.json"))
	require.NoError(t, err)
	assert.NoDirExists(t, filepath.Join(root, "artifacts"))
	// A new store (a reopened window) reads it back.
	again := open(t, root)
	keys, err := again.List("")
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, keys)
	require.NoError(t, again.Delete("a", nil))
	require.NoError(t, again.Flush())
	keys, _ = open(t, root).List("")
	assert.Equal(t, []string{"b"}, keys)
}

func TestStore_Limits(t *testing.T) {
	s := open(t, t.TempDir())
	assert.ErrorIs(t, s.Set("k", raw(`"`+strings.Repeat("a", MaxValueBytes)+`"`), nil), ErrTooLarge, "a value over 64 KB")
	assert.NoError(t, s.Set("k", raw(`"`+strings.Repeat("a", MaxValueBytes-2)+`"`), nil), "exactly 64 KB")
	assert.ErrorIs(t, s.Set("k", raw(`{bad`), nil), ErrBadValue)
	assert.ErrorIs(t, s.Set("k", raw(``), nil), ErrBadValue)
	for _, k := range []string{"", strings.Repeat("k", MaxKeyBytes+1), "a\x00b", "a‮b", "bad\xff"} {
		assert.ErrorIs(t, s.Set(k, raw(`1`), nil), ErrBadKey, "%q", k)
	}
	// The total is 1 MB.
	var err error
	for i := 0; i < 20 && err == nil; i++ {
		err = s.Set("big"+string(rune('a'+i)), raw(`"`+strings.Repeat("b", MaxValueBytes-2)+`"`), nil)
	}
	assert.ErrorIs(t, err, ErrTooLarge)
	// Replacing a value does not double count it.
	assert.NoError(t, s.Set("k", raw(`1`), nil))
}

func TestStore_TwoStoresOnTheSameFileKeepBothWrites(t *testing.T) {
	root := t.TempDir()
	a, b := open(t, root), open(t, root)
	require.NoError(t, a.Set("fromA", raw(`1`), nil))
	require.NoError(t, b.Set("fromB", raw(`2`), nil))
	require.NoError(t, a.Flush())
	require.NoError(t, b.Flush()) // finds the file changed, merges, writes
	a = open(t, root)             // a reopened window sees the merge
	keys, err := a.List("")
	require.NoError(t, err)
	assert.Equal(t, []string{"fromA", "fromB"}, keys)
	// The last writer of one key wins, the others stay.
	require.NoError(t, b.Set("fromA", raw(`3`), nil))
	require.NoError(t, b.Flush())
	v, _, _ := open(t, root).Get("fromA")
	assert.Equal(t, "3", string(v))
}

func TestStore_WritesAreDebounced(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root, "pg", WithDelay(50*time.Millisecond))
	require.NoError(t, err)
	for i := 0; i < 20; i++ {
		require.NoError(t, s.Set("k", raw(`1`), nil))
	}
	assert.NoFileExists(t, filepath.Join(root, ".vibe-local", "artifacts", "pg", "state.json"), "nothing written yet")
	require.Eventually(t, func() bool {
		_, err := os.Stat(filepath.Join(root, ".vibe-local", "artifacts", "pg", "state.json"))
		return err == nil
	}, 2*time.Second, 10*time.Millisecond)
}

func TestStore_SubscribersHearOtherOriginsOnly(t *testing.T) {
	s := open(t, t.TempDir())
	var got []Change
	cancel := s.Subscribe("w1", func(c Change) { got = append(got, c) })
	require.NoError(t, s.Set("a", raw(`1`), "w1"))
	require.NoError(t, s.Set("b", raw(`2`), "w2"))
	require.NoError(t, s.Delete("b", "w2"))
	require.Len(t, got, 2)
	assert.Equal(t, "b", got[0].Key)
	assert.True(t, got[1].Deleted)
	cancel()
	require.NoError(t, s.Set("c", raw(`3`), "w2"))
	assert.Len(t, got, 2)
}

func TestStore_ACorruptOrForeignFileIsRefusedNotReplaced(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, ".vibe-local", "artifacts", "pg")
	require.NoError(t, os.MkdirAll(p, 0o755))
	for _, body := range []string{`not json`, `{"version":2,"values":{}}`, `{"version":1,"values":{},"extra":1}`, `{"version":1,"values":{"":1}}`} {
		require.NoError(t, os.WriteFile(filepath.Join(p, "state.json"), []byte(body), 0o644))
		s := open(t, root)
		_, _, err := s.Get("a")
		assert.ErrorIs(t, err, ErrCorrupt, body)
		assert.ErrorIs(t, s.Set("a", raw(`1`), nil), ErrCorrupt)
		b, _ := os.ReadFile(filepath.Join(p, "state.json"))
		assert.Equal(t, body, string(b), "the file was left alone")
	}
}

func TestOpen_RefusesANameThatIsNotAnArtifactID(t *testing.T) {
	for _, n := range []string{"", "../x", "a/b", "A b", "..", strings.Repeat("a", 200)} {
		_, err := Open(t.TempDir(), n)
		assert.ErrorIs(t, err, ErrBadName, n)
	}
}

func TestStore_HTMLCharactersStayOneByteOnDisk(t *testing.T) {
	root := t.TempDir()
	s := open(t, root)
	// Near the 64 KB cap, made only of characters encoding/json escapes by default.
	val := `"` + strings.Repeat("<&>", (MaxValueBytes-2)/3) + `"`
	require.LessOrEqual(t, len(val), MaxValueBytes)
	require.NoError(t, s.Set("a", raw(val), nil))
	for i := 0; i < 14; i++ { // near the 1 MB total
		require.NoError(t, s.Set("k"+string(rune('a'+i)), raw(val), nil))
	}
	require.NoError(t, s.Flush())
	b, err := os.ReadFile(filepath.Join(root, ".vibe-local", "artifacts", "pg", "state.json"))
	require.NoError(t, err)
	assert.NotContains(t, string(b), `\u003c`, "no escaping")
	assert.Less(t, len(b), MaxTotalBytes+(4<<10), "the file is the size the caps measured")

	// Reopen and write again: nothing is corrupt, nothing is lost.
	again := open(t, root)
	v, ok, err := again.Get("a")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, val, string(v))
	require.NoError(t, again.Set("later", raw(`1`), nil))
	require.NoError(t, again.Flush())
	keys, err := open(t, root).List("")
	require.NoError(t, err)
	assert.Contains(t, keys, "later")
	assert.Len(t, keys, 16)
}

func TestStore_ASymlinkedStateFolderIsRefused(t *testing.T) {
	root := t.TempDir()
	notes := filepath.Join(root, "notes")
	require.NoError(t, os.MkdirAll(notes, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".vibe-local", "artifacts"), 0o755))
	for name, link := range map[string]string{
		"artifact folder": filepath.Join(root, ".vibe-local", "artifacts", "pg"),
		"artifacts":       filepath.Join(root, ".vibe-local", "artifacts"),
	} {
		_ = os.RemoveAll(link)
		require.NoError(t, os.Symlink(map[string]string{"artifact folder": "../../notes", "artifacts": "../notes"}[name], link), name)
		s := open(t, root)
		assert.ErrorIs(t, s.Set("k", raw(`1`), nil), ErrUnsafe, name)
		_, _, err := s.Get("k")
		assert.ErrorIs(t, err, ErrUnsafe, name)
		assert.NoFileExists(t, filepath.Join(notes, "state.json"), name)
		require.NoError(t, os.Remove(link))
		if name == "artifacts" {
			require.NoError(t, os.MkdirAll(link, 0o755))
		}
	}
	// A link as the file itself.
	dir := filepath.Join(root, ".vibe-local", "artifacts", "pg")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.Symlink("../../../notes/state.json", filepath.Join(dir, "state.json")))
	assert.ErrorIs(t, open(t, root).Set("k", raw(`1`), nil), ErrUnsafe)
	assert.NoFileExists(t, filepath.Join(notes, "state.json"))
}

func TestStore_AMergeThatOverflowsKeepsWhatIsOnDiskNotWhatWasNeverWritten(t *testing.T) {
	root := t.TempDir()
	a, b := open(t, root), open(t, root)
	_, _, err := a.Get("x") // a has loaded the (empty) file before b writes
	require.NoError(t, err)
	fill := raw(`"` + strings.Repeat("x", MaxValueBytes-2) + `"`)
	for i := 0; i < 9; i++ {
		require.NoError(t, b.Set("b"+string(rune('a'+i)), fill, nil))
	}
	require.NoError(t, b.Flush())
	for i := 0; i < 9; i++ {
		require.NoError(t, a.Set("a"+string(rune('a'+i)), fill, nil)) // each store is under the cap alone
	}
	assert.ErrorIs(t, a.Flush(), ErrTooLarge, "merged with b's file it does not fit")
	_, shown, err := a.Get("aa")
	require.NoError(t, err)
	assert.False(t, shown, "a value that was never written is not shown as stored")
	_, found, _ := a.Get("ba")
	assert.True(t, found, "memory shows what is on disk")
	keys, err := open(t, root).List("")
	require.NoError(t, err)
	assert.Len(t, keys, 9)
}

func TestStore_LineSeparatorsAreMeasuredInTheFormThatIsWritten(t *testing.T) {
	root := t.TempDir()
	s := open(t, root)
	// 13,000 U+2028 is 39 KB raw and 78 KB as the encoder writes it: over the cap.
	over := `"` + strings.Repeat("\u2028", 13000) + `"`
	assert.ErrorIs(t, s.Set("k", raw(over), nil), ErrTooLarge)
	// Just under the cap in the written form: fine, and the file reopens.
	under := `"` + strings.Repeat("\u2028", 10000) + `"`
	require.NoError(t, s.Set("k", raw(under), nil))
	for i := 0; i < 16; i++ { // up to the total
		_ = s.Set("m"+string(rune('a'+i)), raw(under), nil)
	}
	require.NoError(t, s.Flush())
	b, err := os.ReadFile(filepath.Join(root, ".vibe-local", "artifacts", "pg", "state.json"))
	require.NoError(t, err)
	assert.Less(t, len(b), MaxTotalBytes+(4<<10))
	again := open(t, root)
	v, ok, err := again.Get("k")
	require.NoError(t, err)
	require.True(t, ok)
	assert.LessOrEqual(t, len(v), MaxValueBytes)
	require.NoError(t, again.Set("later", raw(`1`), nil))
	require.NoError(t, again.Flush())
}
