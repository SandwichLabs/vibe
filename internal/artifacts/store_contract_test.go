package artifacts

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const goodView = `{"title":"Sales","components":[{"id":"root","type":"container"}]}`

// storeContract runs the behaviour every Store must share against the file
// store and the in-memory Mock.
func storeContract(t *testing.T, run func(t *testing.T, s Store)) {
	t.Run("file", func(t *testing.T) { run(t, NewFileStore(t.TempDir())) })
	t.Run("mock", func(t *testing.T) { run(t, &Mock{}) })
}

func newArtifact(t *testing.T, name string) *Artifact {
	return &Artifact{Name: name, Manifest: *mustDecode(t, goodManifest), View: []byte(goodView)}
}

func TestStore_CRUDLifecycle(t *testing.T) {
	storeContract(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		a := newArtifact(t, "regional-sales")
		require.NoError(t, s.Put(ctx, a))
		assert.NotEmpty(t, a.Rev)

		got, err := s.Get(ctx, "regional-sales")
		require.NoError(t, err)
		assert.Equal(t, a.Rev, got.Rev)
		assert.Equal(t, "Regional sales", got.Manifest.Title)
		assert.Equal(t, goodView, string(got.View))
		assert.Equal(t, []string{"from", "to", "top"}, got.Manifest.Queries[1].Uses)

		list, err := s.List(ctx)
		require.NoError(t, err)
		require.Len(t, list, 1)
		assert.Equal(t, "regional-sales", list[0].Name)
		assert.Empty(t, list[0].Error)

		got.Manifest.Title = "Sales by region"
		oldRev := got.Rev
		require.NoError(t, s.Put(ctx, got))
		assert.NotEqual(t, oldRev, got.Rev)
		again, err := s.Get(ctx, "regional-sales")
		require.NoError(t, err)
		assert.Equal(t, "Sales by region", again.Manifest.Title)

		require.NoError(t, s.Delete(ctx, "regional-sales"))
		_, err = s.Get(ctx, "regional-sales")
		assert.ErrorIs(t, err, ErrNotFound)
		require.NoError(t, s.Delete(ctx, "regional-sales"), "deleting a missing artifact is a no-op")
	})
}

func TestStore_ListEmptyAndNotFound(t *testing.T) {
	storeContract(t, func(t *testing.T, s Store) {
		list, err := s.List(context.Background())
		require.NoError(t, err)
		assert.NotNil(t, list)
		assert.Empty(t, list)
		_, err = s.Get(context.Background(), "ghost")
		assert.ErrorIs(t, err, ErrNotFound)
	})
}

func TestStore_ListIsSortedByName(t *testing.T) {
	storeContract(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		for _, n := range []string{"zeta", "alpha", "mid"} {
			require.NoError(t, s.Put(ctx, newArtifact(t, n)))
		}
		list, err := s.List(ctx)
		require.NoError(t, err)
		require.Len(t, list, 3)
		assert.Equal(t, []string{"alpha", "mid", "zeta"}, []string{list[0].Name, list[1].Name, list[2].Name})
	})
}

func TestStore_StaleRevConflicts(t *testing.T) {
	storeContract(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		a := newArtifact(t, "sales")
		require.NoError(t, s.Put(ctx, a))
		stale := a.Rev

		other, err := s.Get(ctx, "sales")
		require.NoError(t, err)
		other.Manifest.Title = "Changed elsewhere"
		require.NoError(t, s.Put(ctx, other))

		a.Manifest.Title = "Mine"
		err = s.Put(ctx, a, WithRev(stale))
		require.ErrorIs(t, err, ErrConflict)
		cur, err := s.Get(ctx, "sales")
		require.NoError(t, err)
		assert.Equal(t, "Changed elsewhere", cur.Manifest.Title, "a refused write changes nothing")

		require.NoError(t, s.Put(ctx, a, Overwrite()), "overwrite skips the check")
		cur, err = s.Get(ctx, "sales")
		require.NoError(t, err)
		assert.Equal(t, "Mine", cur.Manifest.Title)
	})
}

func TestStore_ViewOnlyChangeIsAConflict(t *testing.T) {
	storeContract(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		a := newArtifact(t, "sales")
		require.NoError(t, s.Put(ctx, a))
		stale := a.Rev

		other, err := s.Get(ctx, "sales")
		require.NoError(t, err)
		other.View = []byte(`{"title":"Different","components":[{"id":"root","type":"container"}]}`)
		require.NoError(t, s.Put(ctx, other))

		assert.ErrorIs(t, s.Put(ctx, a, WithRev(stale)), ErrConflict, "the revision covers the view too")
	})
}

func TestStore_CreateOverExistingConflicts(t *testing.T) {
	storeContract(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		require.NoError(t, s.Put(ctx, newArtifact(t, "sales")))
		// A second writer that never read it: its blind create must not clobber.
		fresh := newArtifact(t, "sales")
		fresh.Manifest.Title = "Clobber"
		switch v := s.(type) { // a second store over the same data, which has not seen it
		case *FileStore:
			s = NewFileStore(v.root)
		case *Mock:
			s = &Mock{byName: v.byName}
		}
		assert.ErrorIs(t, s.Put(ctx, fresh), ErrConflict)
		got, err := s.Get(ctx, "sales")
		require.NoError(t, err)
		assert.Equal(t, "Regional sales", got.Manifest.Title)
	})
}

func TestStore_PutRefusesInvalid(t *testing.T) {
	storeContract(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		for _, name := range []string{"", "Sales", "a/b", "..", ".", "../x", "con", "nul", "com1", "lpt9", "-x", "a b", ".hidden", "x.y"} {
			a := newArtifact(t, name)
			assert.ErrorIs(t, s.Put(ctx, a), ErrInvalid, "name %q", name)
		}
		bad := newArtifact(t, "ok")
		bad.Manifest.Queries[0].SQL = "SELECT $undeclared"
		assert.ErrorIs(t, s.Put(ctx, bad), ErrInvalid)

		for _, view := range []string{"", "[]", "null", "not json", `{"a":1} {"b":2}`, `{"a":`, `"str"`} {
			a := newArtifact(t, "ok")
			a.View = []byte(view)
			assert.ErrorIs(t, s.Put(ctx, a), ErrInvalid, "view %q", view)
		}
		list, err := s.List(ctx)
		require.NoError(t, err)
		assert.Empty(t, list, "nothing was written")
	})
}

func TestStore_GetRefusesBadNames(t *testing.T) {
	storeContract(t, func(t *testing.T, s Store) {
		for _, name := range []string{"", "..", "../x", "Sales", "a/b", "con", "/etc"} {
			_, err := s.Get(context.Background(), name)
			assert.Error(t, err, name)
		}
	})
}

func TestStore_MockErr(t *testing.T) {
	m := &Mock{Err: assert.AnError}
	_, err := m.List(context.Background())
	assert.ErrorIs(t, err, assert.AnError)
	assert.ErrorIs(t, m.Put(context.Background(), newArtifact(t, "a")), assert.AnError)
}

// WithRev("") is "I loaded nothing": a create-only Put. The revision the store
// last saw must not stand in for it, or a caller that never read an artifact
// (the agent tools) could replace it.
func TestStore_WithEmptyRevIsCreateOnly(t *testing.T) {
	storeContract(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		first := newArtifact(t, "sales")
		require.NoError(t, s.Put(ctx, first, WithRev("")), "creating with an empty rev works")

		_, err := s.Get(ctx, "sales") // the store has now seen the current revision
		require.NoError(t, err)

		again := newArtifact(t, "sales")
		again.Manifest.Title = "Overwritten"
		err = s.Put(ctx, again, WithRev(""))
		require.ErrorIs(t, err, ErrConflict, "an empty rev never replaces an existing artifact")
		got, err := s.Get(ctx, "sales")
		require.NoError(t, err)
		assert.Equal(t, "Regional sales", got.Manifest.Title)

		// Without the option the last-seen fallback still works for read-then-write code.
		got.Manifest.Title = "Edited"
		require.NoError(t, s.Put(ctx, got))
	})
}
