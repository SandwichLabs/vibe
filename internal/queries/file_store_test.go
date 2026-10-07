package queries

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "rewrite golden files")

func goldenQuery() Query {
	return Query{
		ID: "top-customers", Title: "Top customers", Description: "Biggest spenders <last 90 days> & more",
		Connection: "prod_pg",
		SQL:        "SELECT customer, sum(amt) AS total\nFROM orders\nWHERE amt > $min\nGROUP BY 1\nORDER BY 2 DESC\nLIMIT 10",
		Parameters: []Param{{Name: "min", Type: "number", Required: true, Default: "100"}},
	}
}

func TestEncode_Golden(t *testing.T) {
	got, err := Encode(goldenQuery())
	require.NoError(t, err)
	path := filepath.Join("testdata", "golden", "top-customers.sql")
	if *update {
		require.NoError(t, os.WriteFile(path, got, 0o644))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(want), string(got))
	assert.True(t, strings.HasPrefix(string(got), "-- vibe: {"))
	assert.True(t, strings.HasSuffix(string(got), "LIMIT 10\n"))
	assert.NotContains(t, string(got), "\\u003c")
}

func TestFileStore_SaveLoadSaveIsByteIdentical(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s := NewFileStore(root)
	golden, err := os.ReadFile(filepath.Join("testdata", "golden", "top-customers.sql"))
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "queries"), 0o755))
	path := filepath.Join(root, "queries", "top-customers.sql")
	require.NoError(t, os.WriteFile(path, golden, 0o644))

	for i := 0; i < 3; i++ {
		q, err := s.Get(ctx, "top-customers")
		require.NoError(t, err)
		assert.Equal(t, "prod_pg", q.Connection)
		assert.Equal(t, "min", q.Parameters[0].Name)
		require.NoError(t, s.Update(ctx, q))
		after, _ := os.ReadFile(path)
		assert.Equal(t, string(golden), string(after), "cycle %d", i)
	}
}

func TestFileStore_CreateAssignsSlugIDAndDoesNotClobber(t *testing.T) {
	ctx := context.Background()
	s := NewFileStore(t.TempDir())
	a := &Query{ID: "6f1c9b2e-0000-4000-8000-000000000000", Title: "Daily Sales", SQL: "select 1"}
	require.NoError(t, s.Create(ctx, a))
	assert.Equal(t, "daily-sales", a.ID, "a uuid-shaped id is replaced by the slug")
	b := &Query{Title: "Daily Sales", SQL: "select 2"}
	require.NoError(t, s.Create(ctx, b))
	assert.Equal(t, "daily-sales-2", b.ID)

	explicit := &Query{ID: "mine", Title: "T", SQL: "select 3"}
	require.NoError(t, s.Create(ctx, explicit))
	assert.Equal(t, "mine", explicit.ID)
	dup := &Query{ID: "mine", Title: "T2", SQL: "select 4"}
	assert.ErrorIs(t, s.Create(ctx, dup), ErrConflict, "creating over an existing file is a conflict")

	list, err := s.List(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 3)
}

func TestFileStore_HeaderlessAndHandEditedFiles(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s := NewFileStore(root)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "queries"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "queries", "plain.sql"), []byte("select 1;\r\n"), 0o644))
	q, err := s.Get(ctx, "plain")
	require.NoError(t, err)
	assert.Equal(t, "plain", q.Title)
	assert.Equal(t, "select 1;", q.SQL)
	assert.Equal(t, "duckdb", q.Engine)
	assert.Empty(t, q.Parameters)
	assert.NotNil(t, q.Parameters)

	require.NoError(t, os.WriteFile(filepath.Join(root, "queries", "broken.sql"), []byte("-- vibe: {nope\nselect 1\n"), 0o644))
	gb, err := s.Get(ctx, "broken")
	require.NoError(t, err, "a malformed header still opens")
	assert.Contains(t, gb.Error, "header could not be read")
	assert.Contains(t, gb.SQL, "select 1", "the raw file is returned as the SQL")
	list, err := s.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2, "an unreadable file still lists, so it can be found and fixed")
	var bad Query
	for _, q := range list {
		if q.ID == "broken" {
			bad = q
		}
	}
	assert.Contains(t, bad.Error, "header could not be read")
	assert.Equal(t, "broken", bad.Title)

	// A save over it is refused and the file is untouched: parameters that
	// failed to parse are never silently dropped.
	before, _ := os.ReadFile(filepath.Join(root, "queries", "broken.sql"))
	err = s.Update(ctx, &Query{ID: "broken", Title: "Fixed?", SQL: "select 9"}, Overwrite())
	assert.Error(t, err)
	after, _ := os.ReadFile(filepath.Join(root, "queries", "broken.sql"))
	assert.Equal(t, string(before), string(after))

	// Wrong-shaped parameters are a parse failure too, not "no parameters".
	require.NoError(t, os.WriteFile(filepath.Join(root, "queries", "shape.sql"), []byte("-- vibe: {\"title\":\"S\",\"parameters\":{\"name\":\"x\"}}\nselect 1\n"), 0o644))
	sh, err := s.Get(ctx, "shape")
	require.NoError(t, err)
	assert.NotEmpty(t, sh.Error)
	assert.Error(t, s.Update(ctx, &Query{ID: "shape", Title: "S", SQL: "select 2"}), "a save that would drop the unparsed parameters is refused")
}

func TestFileStore_BOMAndLeadingWhitespaceBeforeHeader(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s := NewFileStore(root)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "queries"), 0o755))
	w := func(name, content string) {
		require.NoError(t, os.WriteFile(filepath.Join(root, "queries", name), []byte(content), 0o644))
	}
	w("bom.sql", "\xEF\xBB\xBF-- vibe: {\"title\":\"Bom\",\"parameters\":[{\"name\":\"p\",\"type\":\"string\"}]}\r\nSELECT 1\r\n")
	w("spaced.sql", "\n  \t-- vibe: {\"title\":\"Spaced\"}\nSELECT 3\n")
	b, err := s.Get(ctx, "bom")
	require.NoError(t, err)
	assert.Equal(t, "Bom", b.Title)
	require.Len(t, b.Parameters, 1)
	assert.Equal(t, "SELECT 1", b.SQL)
	sp, err := s.Get(ctx, "spaced")
	require.NoError(t, err)
	assert.Equal(t, "Spaced", sp.Title)

	// Saving normalises: the BOM and indentation are gone, the parameter kept.
	require.NoError(t, s.Update(ctx, b))
	data, _ := os.ReadFile(filepath.Join(root, "queries", "bom.sql"))
	assert.True(t, strings.HasPrefix(string(data), "-- vibe: {"))
	assert.Contains(t, string(data), `"parameters":[{"name":"p"`)
}

func TestFileStore_InvalidFileNamesAndOversizeListWithError(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s := NewFileStore(root)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "queries"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "queries", "My Query.sql"), []byte("select 1"), 0o644))
	f, err := os.Create(filepath.Join(root, "queries", "huge.sql"))
	require.NoError(t, err)
	require.NoError(t, f.Truncate(11<<20))
	require.NoError(t, f.Close())
	list, err := s.List(ctx)
	require.NoError(t, err)
	errs := map[string]string{}
	for _, q := range list {
		errs[q.ID] = q.Error
	}
	assert.Contains(t, errs["My Query"], "not a valid query id")
	assert.Contains(t, errs["huge"], "too large")
	_, err = s.Get(ctx, "My Query")
	assert.Error(t, err)
}

func TestFileStore_ConflictOnStaleUpdate(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s := NewFileStore(root)
	q := &Query{Title: "Q", SQL: "select 1"}
	require.NoError(t, s.Create(ctx, q))

	mine, err := s.Get(ctx, q.ID)
	require.NoError(t, err)

	path := filepath.Join(root, "queries", q.ID+".sql")
	require.NoError(t, os.WriteFile(path, []byte("-- vibe: {\"title\":\"Q\"}\nselect 'theirs'\n"), 0o644))

	mine.SQL = "select 'mine'"
	err = s.Update(ctx, mine)
	require.ErrorIs(t, err, ErrConflict)
	disk, _ := os.ReadFile(path)
	assert.Contains(t, string(disk), "theirs")

	require.NoError(t, s.Update(ctx, mine, Overwrite()))
	disk, _ = os.ReadFile(path)
	assert.Contains(t, string(disk), "mine")

	// Blind update of a file this store never read.
	other := NewFileStore(root)
	blind := &Query{ID: q.ID, Title: "Q", SQL: "select 'blind'"}
	assert.ErrorIs(t, other.Update(ctx, blind), ErrConflict)
	require.NoError(t, other.Update(ctx, blind, WithRev(mine.Rev)), "an explicit matching rev is accepted")
}

func TestFileStore_UpdateAndDeleteMissing(t *testing.T) {
	ctx := context.Background()
	s := NewFileStore(t.TempDir())
	err := s.Update(ctx, &Query{ID: "ghost", Title: "G", SQL: "x"})
	assert.ErrorIs(t, err, ErrNotFound)
	assert.ErrorIs(t, s.Delete(ctx, "ghost"), ErrNotFound)

	q := &Query{Title: "Del", SQL: "x"}
	require.NoError(t, s.Create(ctx, q))
	require.NoError(t, s.Delete(ctx, q.ID))
	_, err = s.Get(ctx, q.ID)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestFileStore_RejectsBadInput(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s := NewFileStore(root)
	assert.Error(t, s.Create(ctx, &Query{Title: "", SQL: "x"}))
	assert.Error(t, s.Create(ctx, &Query{Title: "T", SQL: "x", Connection: "Bad Name!"}))
	assert.Error(t, s.Create(ctx, &Query{ID: "../escape", Title: "T", SQL: "x"}))
	_, err := s.Get(ctx, "../etc/passwd")
	assert.Error(t, err)
	assert.NoFileExists(t, filepath.Join(filepath.Dir(root), "escape.sql"))
}

func TestFileStore_ObserverSeesWrites(t *testing.T) {
	var path, hash string
	s := NewFileStore(t.TempDir(), WithObserver(func(p, h string) { path, hash = p, h }))
	q := &Query{Title: "Obs", SQL: "x"}
	require.NoError(t, s.Create(context.Background(), q))
	assert.True(t, strings.HasSuffix(path, "obs.sql"))
	assert.Equal(t, q.Rev, hash)
}

func TestNotFoundError_MatchesErrNotFound(t *testing.T) {
	err := error(&notFoundError{id: "x"})
	assert.ErrorIs(t, err, ErrNotFound)
}
