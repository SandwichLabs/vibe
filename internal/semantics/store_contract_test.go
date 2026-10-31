package semantics

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// storeContract runs the behaviour every Store must share against the file
// store and the in-memory Mock.
func storeContract(t *testing.T, run func(t *testing.T, s Store)) {
	t.Run("file", func(t *testing.T) { run(t, NewFileStore(t.TempDir())) })
	t.Run("mock", func(t *testing.T) { run(t, &Mock{}) })
}

func mustMetric(t *testing.T) *Metric {
	t.Helper()
	m, err := DecodeMetric("net_revenue", []byte(goodMetric))
	require.NoError(t, err)
	m.Rev = ""
	return m
}

func TestStore_ListEmpty(t *testing.T) {
	storeContract(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		es, err := s.Entities(ctx)
		require.NoError(t, err)
		assert.NotNil(t, es)
		assert.Empty(t, es)
		ms, err := s.Metrics(ctx)
		require.NoError(t, err)
		assert.NotNil(t, ms)
		js, err := s.Joins(ctx)
		require.NoError(t, err)
		assert.NotNil(t, js)
		g, err := s.Glossary(ctx)
		require.NoError(t, err)
		assert.Empty(t, g.Rev)
		assert.Empty(t, g.Terms)
		assert.NotNil(t, g.Terms)
	})
}

func TestStore_CRUDLifecycle(t *testing.T) {
	storeContract(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		m := mustMetric(t)
		require.NoError(t, s.PutMetric(ctx, m))
		assert.NotEmpty(t, m.Rev)

		got, err := s.Metric(ctx, "net_revenue")
		require.NoError(t, err)
		assert.Equal(t, m.Rev, got.Rev)
		assert.Equal(t, "sum(amount)", got.Measure)

		list, err := s.Metrics(ctx)
		require.NoError(t, err)
		require.Len(t, list, 1)
		assert.Equal(t, m.Rev, list[0].Rev)

		m.Title = "Net revenue (after refunds)"
		require.NoError(t, s.PutMetric(ctx, m), "the Rev it was saved with is the revision to write against")
		got, err = s.Metric(ctx, "net_revenue")
		require.NoError(t, err)
		assert.Equal(t, "Net revenue (after refunds)", got.Title)

		require.NoError(t, s.DeleteMetric(ctx, "net_revenue", WithRev(got.Rev)))
		_, err = s.Metric(ctx, "net_revenue")
		assert.ErrorIs(t, err, ErrNotFound)
		assert.ErrorIs(t, s.DeleteMetric(ctx, "net_revenue"), ErrNotFound)
	})
}

func TestStore_AllKinds(t *testing.T) {
	storeContract(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		e, err := DecodeEntity("customer", []byte(goodEntity))
		require.NoError(t, err)
		e.Rev = ""
		require.NoError(t, s.PutEntity(ctx, e))
		j, err := DecodeJoin("orders_customers", []byte(goodJoin))
		require.NoError(t, err)
		j.Rev = ""
		require.NoError(t, s.PutJoin(ctx, j))
		g, err := DecodeGlossary([]byte(goodGlossary))
		require.NoError(t, err)
		require.NoError(t, s.PutGlossary(ctx, g))

		ge, err := s.Entity(ctx, "customer")
		require.NoError(t, err)
		assert.Equal(t, e.Table, ge.Table)
		gj, err := s.Join(ctx, "orders_customers")
		require.NoError(t, err)
		assert.Equal(t, j.Measured, gj.Measured)
		gg, err := s.Glossary(ctx)
		require.NoError(t, err)
		assert.Equal(t, goodGlossary, gg.Text)
		assert.Equal(t, []string{"Active customer", "Refund"}, gg.Terms)
		assert.Equal(t, g.Rev, gg.Rev)

		require.NoError(t, s.DeleteEntity(ctx, "customer"))
		require.NoError(t, s.DeleteJoin(ctx, "orders_customers"))
	})
}

func TestStore_GetUnknownAndBadNames(t *testing.T) {
	storeContract(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		_, err := s.Entity(ctx, "nope")
		assert.ErrorIs(t, err, ErrNotFound)
		assert.Contains(t, err.Error(), "not found")
		for _, bad := range []string{"../x", "Customer", "a/b", ""} {
			_, err := s.Metric(ctx, bad)
			assert.ErrorIs(t, err, ErrInvalid, bad)
			assert.NotErrorIs(t, err, ErrNotFound, bad)
		}
	})
}

func TestStore_CompareAndWrite(t *testing.T) {
	storeContract(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		first := mustMetric(t)
		require.NoError(t, s.PutMetric(ctx, first))

		t.Run("create over existing conflicts", func(t *testing.T) {
			again := mustMetric(t)
			assert.ErrorIs(t, s.PutMetric(ctx, again), ErrConflict)
			assert.ErrorIs(t, s.PutMetric(ctx, again, WithRev("")), ErrConflict)
		})
		t.Run("stale rev conflicts and changes nothing", func(t *testing.T) {
			stale := *first
			other := *first
			other.Title = "someone else"
			require.NoError(t, s.PutMetric(ctx, &other))
			stale.Title = "mine"
			assert.ErrorIs(t, s.PutMetric(ctx, &stale), ErrConflict)
			got, err := s.Metric(ctx, "net_revenue")
			require.NoError(t, err)
			assert.Equal(t, "someone else", got.Title)
			assert.ErrorIs(t, s.DeleteMetric(ctx, "net_revenue", WithRev(stale.Rev)), ErrConflict)
		})
		t.Run("overwrite wins", func(t *testing.T) {
			stale := mustMetric(t)
			stale.Title = "forced"
			require.NoError(t, s.PutMetric(ctx, stale, WithOverwrite()))
			got, err := s.Metric(ctx, "net_revenue")
			require.NoError(t, err)
			assert.Equal(t, "forced", got.Title)
		})
		t.Run("WithRev overrides the item's rev", func(t *testing.T) {
			cur, err := s.Metric(ctx, "net_revenue")
			require.NoError(t, err)
			bogus := *cur
			bogus.Rev = "stale"
			bogus.Title = "via WithRev"
			require.NoError(t, s.PutMetric(ctx, &bogus, WithRev(cur.Rev)))
		})
		t.Run("a write to a deleted file conflicts", func(t *testing.T) {
			cur, err := s.Metric(ctx, "net_revenue")
			require.NoError(t, err)
			require.NoError(t, s.DeleteMetric(ctx, "net_revenue"))
			assert.ErrorIs(t, s.PutMetric(ctx, cur), ErrConflict)
		})
		t.Run("glossary follows the same rules", func(t *testing.T) {
			g := &Glossary{Text: "## a\n"}
			require.NoError(t, s.PutGlossary(ctx, g))
			assert.ErrorIs(t, s.PutGlossary(ctx, &Glossary{Text: "## b\n"}), ErrConflict)
			g.Text = "## a\n\nmore\n"
			require.NoError(t, s.PutGlossary(ctx, g))
		})
	})
}

func TestStore_PutRefusesInvalid(t *testing.T) {
	storeContract(t, func(t *testing.T, s Store) {
		m := mustMetric(t)
		m.Measure = "sum(x); drop table y"
		assert.ErrorIs(t, s.PutMetric(context.Background(), m), ErrInvalid)
		ms, err := s.Metrics(context.Background())
		require.NoError(t, err)
		assert.Empty(t, ms)
	})
}

func TestStore_UnreadableFilesListWithError(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, Dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	}
	files := map[string]string{
		"metrics/net_revenue.yaml": goodMetric,
		"metrics/broken.yaml":      "version: 1\nname: broken\nquery: q\n",
		"metrics/anchor.yaml":      "version: 1\nname: &x anchor\n",
		"metrics/mismatch.yaml":    goodMetric,
		"metrics/Bad-Name.yaml":    goodMetric,
		"metrics/.hidden.yaml":     goodMetric,
		"metrics/notes.txt":        "ignored",
	}
	for rel, body := range files {
		write(rel, body)
	}
	for name, s := range map[string]Store{"file": NewFileStore(root), "mock": mockWith(files)} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			list, err := s.Metrics(ctx)
			require.NoError(t, err)
			byName := map[string]Metric{}
			var order []string
			for _, m := range list {
				byName[m.Name] = m
				order = append(order, m.Name)
			}
			assert.Equal(t, []string{"Bad-Name", "anchor", "broken", "mismatch", "net_revenue"}, order, "sorted, unreadable ones included, hidden and non-yaml files not")
			assert.Empty(t, byName["net_revenue"].Error)
			assert.NotEmpty(t, byName["net_revenue"].Rev)
			assert.Contains(t, byName["broken"].Error, "measure is required")
			assert.Contains(t, byName["anchor"].Error, "anchors")
			assert.Contains(t, byName["mismatch"].Error, "does not match the file name")
			assert.Contains(t, byName["Bad-Name"].Error, "not a valid definition name")
			assert.NotContains(t, byName["broken"].Error, "invalid semantics definition", "the sentinel prefix is not repeated in every listed Error")

			_, err = s.Metric(ctx, "broken")
			assert.ErrorIs(t, err, ErrInvalid)

			// Saving over it needs Overwrite.
			fix := mustMetric(t)
			fix.Name = "broken"
			err = s.PutMetric(ctx, fix)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "could not be read")
			require.NoError(t, s.PutMetric(ctx, fix, WithOverwrite()))
			got, err := s.Metric(ctx, "broken")
			require.NoError(t, err)
			assert.Empty(t, got.Error)
		})
	}
}

func mockWith(files map[string]string) *Mock {
	m := &Mock{}
	for rel, body := range files {
		m.SetFile(rel, []byte(body))
	}
	return m
}

func TestStore_GlossaryUnreadableListsWithError(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, Dir), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, Dir, GlossaryFile), []byte("## a\x1b\n"), 0o644))
	for name, s := range map[string]Store{"file": NewFileStore(root), "mock": mockWith(map[string]string{"glossary.md": "## a\x1b\n"})} {
		t.Run(name, func(t *testing.T) {
			g, err := s.Glossary(context.Background())
			require.NoError(t, err)
			assert.Contains(t, g.Error, "control character")
			assert.NotEmpty(t, g.Rev)
			assert.Error(t, s.PutGlossary(context.Background(), &Glossary{Text: "## b\n"}, WithRev(g.Rev)))
			require.NoError(t, s.PutGlossary(context.Background(), &Glossary{Text: "## b\n"}, WithRev(g.Rev), WithOverwrite()))
		})
	}
}

func TestStore_TooManyFilesListsTheFirstOnesAndSaysSo(t *testing.T) {
	m := &Mock{}
	total := MaxFilesPerKind + 7
	for i := 0; i < total; i++ {
		m.SetFile(fmt.Sprintf("joins/j%04d.yaml", i), []byte("x"))
	}
	list, err := m.Joins(context.Background())
	require.NoError(t, err)
	require.Len(t, list, MaxFilesPerKind+1)
	last := list[len(list)-1]
	assert.Equal(t, TruncatedStubName, last.Name)
	assert.Contains(t, last.Error, "7 more files")
	assert.Equal(t, "j0000", list[0].Name)
}

func TestFileStore_SymlinkedFileListsWithAnError(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, Dir, "metrics")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	target := filepath.Join(root, "elsewhere.yaml")
	require.NoError(t, os.WriteFile(target, []byte(goodMetric), 0o644))
	require.NoError(t, os.Symlink(target, filepath.Join(dir, "net_revenue.yaml")))
	s := NewFileStore(root)
	list, err := s.Metrics(context.Background())
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Contains(t, list[0].Error, "symbolic link")
	_, err = s.Metric(context.Background(), "net_revenue")
	assert.Error(t, err)
}

func TestStore_CaseVariantFileBlocksAWrite(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, Dir, "entities"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, Dir, "entities", "Customer.yaml"), []byte("x"), 0o644))
	for name, s := range map[string]Store{"file": NewFileStore(root), "mock": mockWith(map[string]string{"entities/Customer.yaml": "x"})} {
		t.Run(name, func(t *testing.T) {
			e, err := DecodeEntity("customer", []byte(goodEntity))
			require.NoError(t, err)
			err = s.PutEntity(context.Background(), e)
			require.ErrorIs(t, err, ErrConflict)
			assert.Contains(t, err.Error(), "different capitalisation")
			_, err = s.PutSource(context.Background(), KindEntity, "customer", []byte(goodEntity))
			assert.ErrorIs(t, err, ErrConflict)
			list, err := s.Entities(context.Background())
			require.NoError(t, err)
			require.Len(t, list, 1)
			assert.NotEmpty(t, list[0].Error, "the variant lists as invalid")
		})
	}
}

func TestStore_ErrFailsEveryCall(t *testing.T) {
	boom := errors.New("boom")
	m := &Mock{Err: boom}
	ctx := context.Background()
	_, err := m.Entities(ctx)
	assert.ErrorIs(t, err, boom)
	_, err = m.Metric(ctx, "x")
	assert.ErrorIs(t, err, boom)
	assert.ErrorIs(t, m.PutMetric(ctx, mustMetric(t)), boom)
	_, err = m.Glossary(ctx)
	assert.ErrorIs(t, err, boom)
}

func TestFileStore_LayoutAndObserver(t *testing.T) {
	root := t.TempDir()
	var seen []string
	s := NewFileStore(root, WithObserver(func(path, hash string) func() { seen = append(seen, filepath.Base(path)+":"+hash); return nil }))
	m := mustMetric(t)
	require.NoError(t, s.PutMetric(context.Background(), m))
	data, err := os.ReadFile(filepath.Join(root, "semantics", "metrics", "net_revenue.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "version: 1\nname: net_revenue\n")
	require.Len(t, seen, 1)
	assert.Contains(t, seen[0], "net_revenue.yaml:")
	entries, err := os.ReadDir(filepath.Join(root, "semantics", "metrics"))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temp files left behind")
}

func TestFileStore_SaveLoadSaveIsByteIdentical(t *testing.T) {
	root := t.TempDir()
	s := NewFileStore(root)
	require.NoError(t, s.PutMetric(context.Background(), mustMetric(t)))
	p := filepath.Join(root, "semantics", "metrics", "net_revenue.yaml")
	first, _ := os.ReadFile(p)
	for i := 0; i < 3; i++ {
		m, err := s.Metric(context.Background(), "net_revenue")
		require.NoError(t, err)
		require.NoError(t, s.PutMetric(context.Background(), m))
		after, _ := os.ReadFile(p)
		assert.Equal(t, string(first), string(after), "cycle %d", i)
	}
}

func TestStore_PutSourceKeepsBytes(t *testing.T) {
	storeContract(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		handWritten := "# kept as written, comment and all\n" + goodMetric
		rev, err := s.PutSource(ctx, KindMetric, "net_revenue", []byte(handWritten))
		require.NoError(t, err)
		got, err := s.Metric(ctx, "net_revenue")
		require.NoError(t, err)
		assert.Equal(t, rev, got.Rev)
		if fs, ok := s.(*FileStore); ok {
			b, _, _ := fs.b.read("semantics/metrics/net_revenue.yaml")
			assert.Equal(t, handWritten, string(b))
		}

		_, err = s.PutSource(ctx, KindMetric, "net_revenue", []byte(handWritten))
		assert.ErrorIs(t, err, ErrConflict, "create-only without a revision")
		_, err = s.PutSource(ctx, KindMetric, "net_revenue", []byte(handWritten), WithRev(rev))
		assert.NoError(t, err)

		_, err = s.PutSource(ctx, KindMetric, "other", []byte(goodMetric))
		assert.ErrorIs(t, err, ErrInvalid, "the name inside must match")
		_, err = s.PutSource(ctx, KindMetric, "x", []byte("version: 1\n"))
		assert.ErrorIs(t, err, ErrInvalid)
		_, err = s.PutSource(ctx, KindGlossary, "", []byte("## a\n"))
		assert.NoError(t, err)
		_, err = s.PutSource(ctx, Kind("nope"), "x", nil)
		assert.ErrorIs(t, err, ErrInvalid)
	})
}

func TestStore_SourceReturnsTheFileAsStored(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, Dir, "metrics"), 0o755))
	broken := "version: 1\nname: broken\n# not valid\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, Dir, "metrics", "broken.yaml"), []byte(broken), 0o644))
	for name, s := range map[string]Store{"file": NewFileStore(root), "mock": mockWith(map[string]string{"metrics/broken.yaml": broken})} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			src, err := s.Source(ctx, KindMetric, "broken")
			require.NoError(t, err)
			assert.Equal(t, broken, src.Text)
			assert.NotEmpty(t, src.Rev)
			_, err = s.Source(ctx, KindMetric, "absent")
			assert.ErrorIs(t, err, ErrNotFound)
			_, err = s.Source(ctx, KindGlossary, "")
			assert.ErrorIs(t, err, ErrNotFound)
			_, err = s.Source(ctx, KindMetric, "../x")
			assert.ErrorIs(t, err, ErrInvalid)
			_, err = s.Source(ctx, Kind("nope"), "x")
			assert.ErrorIs(t, err, ErrInvalid)
		})
	}
}
