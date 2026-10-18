package trustfile

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecisions_AllowIsKeyedByPathAndHash(t *testing.T) {
	d := New("")
	assert.Equal(t, StateUndecided, d.State("/a", "h1"))
	require.NoError(t, d.Allow("/a", "h1"))
	assert.Equal(t, StateAllowed, d.State("/a", "h1"))
	assert.Equal(t, StateChanged, d.State("/a", "h2"), "any other hash is a change")
	assert.Equal(t, StateUndecided, d.State("/b", "h1"), "another path never shares it")
	require.NoError(t, d.Revoke("/a"))
	assert.Equal(t, StateUndecided, d.State("/a", "h1"))
}

func TestDecisions_DenyHoldsAcrossContent(t *testing.T) {
	d := New("")
	require.NoError(t, d.Deny("/a"))
	assert.Equal(t, StateDenied, d.State("/a", "h1"))
	assert.Equal(t, StateDenied, d.State("/a", "h2"))
	require.NoError(t, d.Allow("/a", "h2"))
	assert.Equal(t, StateAllowed, d.State("/a", "h2"), "an allow replaces a deny")
}

func TestDecisions_FileFormatAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "views.json")
	d := New(path)
	require.NoError(t, d.Allow("/a", "h1"))
	require.NoError(t, d.Deny("/b"))
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.JSONEq(t, `{"version":1,"files":{"/a":{"decision":"allow","sha256":"h1"},"/b":{"decision":"deny"}}}`, string(raw))
	again := New(path)
	assert.Equal(t, StateAllowed, again.State("/a", "h1"))
	assert.Equal(t, StateDenied, again.State("/b", "x"))
	assert.Equal(t, []Entry{{Path: "/a", Decision: "allow", SHA256: "h1"}, {Path: "/b", Decision: "deny"}}, again.List())
	ents, _ := os.ReadDir(filepath.Dir(path))
	for _, e := range ents {
		assert.NotContains(t, e.Name(), ".tmp", "no temp file is left behind")
	}
}

func TestDecisions_AllowIfRefusesAStaleHash(t *testing.T) {
	d := New("")
	now := "h2"
	cur := func() (string, error) { return now, nil }
	require.ErrorIs(t, d.AllowIf("/a", "h1", cur), ErrStale)
	assert.Equal(t, StateUndecided, d.State("/a", "h1"), "nothing was recorded")
	require.ErrorIs(t, d.AllowIf("/a", "", cur), ErrStale, "no hash is never an allow")
	require.NoError(t, d.AllowIf("/a", "h2", cur))
	assert.Equal(t, StateAllowed, d.State("/a", "h2"))
}

func TestDecisions_CorruptFileIsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v.json")
	require.NoError(t, os.WriteFile(path, []byte("{not json"), 0o600))
	assert.Equal(t, StateUndecided, New(path).State("/a", "h"))
}

func TestDecisions_TwoInstancesOnOneFileLoseNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "views.json")
	a, b := New(path), New(path) // two "processes": separate instances, separate mutexes
	const n = 40
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(2)
		go func(i int) { defer wg.Done(); assert.NoError(t, a.Allow(fmt.Sprintf("/a/%d", i), "h")) }(i)
		go func(i int) { defer wg.Done(); assert.NoError(t, b.Deny(fmt.Sprintf("/b/%d", i))) }(i)
	}
	wg.Wait()
	assert.Len(t, New(path).List(), 2*n, "no decision was overwritten by the other instance")

	// Revoke and AllowIf also run under the lock.
	require.NoError(t, a.Revoke("/a/0"))
	require.NoError(t, b.AllowIf("/c", "h", func() (string, error) { return "h", nil }))
	assert.Len(t, New(path).List(), 2*n)
}
