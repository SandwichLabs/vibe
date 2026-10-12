package engine

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/session"
)

func hubWithOther(t *testing.T) session.Session {
	t.Helper()
	dir := t.TempDir()
	s, err := session.New(
		session.WithTempDirectory(filepath.Join(dir, "tmp")),
		session.WithExtensionDirectory(filepath.Join(dir, "ext")),
		session.WithSecretDirectory(filepath.Join(dir, "sec")),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	path := filepath.Join(dir, "other.duckdb")
	f, err := sql.Open("duckdb", path)
	require.NoError(t, err)
	_, err = f.Exec(`CREATE SEQUENCE _vibe_seq_t_id START 1;
		CREATE TABLE t (id BIGINT PRIMARY KEY DEFAULT nextval('_vibe_seq_t_id'), v VARCHAR);
		CREATE TABLE parent (id INTEGER PRIMARY KEY);
		CREATE TABLE child (pid INTEGER REFERENCES parent(id));
		INSERT INTO t (v) VALUES ('a'), ('b');`)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.NoError(t, s.Attach(context.Background(), session.Spec{Name: "other", Path: path}))
	return s
}

func TestTableColumns_SecondCatalog(t *testing.T) {
	s := hubWithOther(t)
	_, err := s.DB().Exec("CREATE TABLE t (unrelated INTEGER)")
	require.NoError(t, err)

	cols, err := TableColumns(s.DB(), session.TableRef{Catalog: "other", Schema: "main", Name: "t"})
	require.NoError(t, err)
	require.Len(t, cols, 2)
	assert.Equal(t, "id", cols[0].Name)
	assert.True(t, cols[0].PK)
	assert.Contains(t, cols[0].Default, "nextval")

	def, err := TableColumns(s.DB(), session.TableRef{Name: "t"})
	require.NoError(t, err)
	require.Len(t, def, 1)
	assert.Equal(t, "unrelated", def[0].Name)

	child, err := TableColumns(s.DB(), session.TableRef{Catalog: "other", Name: "child"})
	require.NoError(t, err)
	require.Len(t, child, 1)
	assert.True(t, child[0].FK)
}

func TestAutoNumber_SecondCatalog(t *testing.T) {
	ctx := context.Background()
	s := hubWithOther(t)
	ref := session.TableRef{Catalog: "other", Schema: "main", Name: "t"}

	// Dispense once in this session so the sequence reports a live position
	// (a sequence reloaded from a file reports its next value instead).
	var seen int64
	require.NoError(t, s.DB().QueryRow("SELECT nextval('other.main._vibe_seq_t_id')").Scan(&seen))
	_, err := s.DB().Exec("INSERT INTO other.main.t (id, v) VALUES (50, 'far')")
	require.NoError(t, err)
	gap, ok, err := AutoNumberGap(ctx, s.DB(), ref, "id", 50)
	require.NoError(t, err)
	require.True(t, ok)
	// last_value == start_value is ambiguous (#19), so the gap may overshoot by one, never undershoot.
	assert.EqualValues(t, 50-seen+1, gap)

	require.NoError(t, BumpAutoNumber(ctx, s.DB(), ref, "id"))
	var next int64
	require.NoError(t, s.DB().QueryRow("INSERT INTO other.main.t (v) VALUES ('z') RETURNING id").Scan(&next))
	assert.Greater(t, next, int64(50), "the sequence in the other catalog was advanced past the data")

	// A same-named column in the default catalog has no sequence: not found there.
	_, ok, err = AutoNumberGap(ctx, s.DB(), session.TableRef{Name: "t"}, "id", 5)
	if err == nil {
		assert.False(t, ok)
	}
}

func TestResolveTable(t *testing.T) {
	ctx := context.Background()
	s := hubWithOther(t)

	ref, err := ResolveTable(ctx, s, "t")
	require.NoError(t, err)
	assert.Equal(t, session.TableRef{Catalog: "memory", Schema: "main", Name: "t"}, ref)
	ref, err = ResolveTable(ctx, s, "other.t")
	require.NoError(t, err)
	assert.Equal(t, session.TableRef{Catalog: "other", Schema: "main", Name: "t"}, ref, "x.t with x an attached catalog")
	ref, err = ResolveTable(ctx, s, "other.main.t")
	require.NoError(t, err)
	assert.Equal(t, "other", ref.Catalog)

	for _, bad := range []string{"", "a.b.c.d", "_vibe_x", "other.main._vibe_x", "t; DROP", `t"`, "1t", "bad catalog.main.t"} {
		_, err := ResolveTable(ctx, s, bad)
		assert.Error(t, err, bad)
	}
	assert.Equal(t, `"other"."main"."t"`, ref.SQL())
	assert.Equal(t, `"t"`, TableSQL("t"))
	assert.Equal(t, `"c"."s"."t"`, TableSQL("c.s.t"))
	assert.Equal(t, `"a""b"`, TableSQL(`a"b`))
}

func TestManager_ExecuteCappedIn(t *testing.T) {
	ctx := context.Background()
	s := hubWithOther(t)
	m := NewManager(s)

	rs, _, err := m.ExecuteCappedIn(ctx, "other", "SELECT v FROM t ORDER BY v", nil, 0)
	require.NoError(t, err)
	assert.Equal(t, 2, rs.RowCount(), "unqualified t is other's t")

	_, _, err = m.ExecuteCappedIn(ctx, "", "SELECT v FROM t", nil, 0)
	assert.Error(t, err, "no t in the default catalog")

	_, _, err = m.ExecuteCappedIn(ctx, "ghost", "SELECT 1", nil, 0)
	assert.Error(t, err)

	rs, err = m.ExecuteIn(ctx, "other", "SELECT current_database()", nil)
	require.NoError(t, err)
	assert.Equal(t, "other", rs.Rows[0][0])
	rs, err = m.Execute(ctx, "SELECT current_database()", nil)
	require.NoError(t, err)
	assert.Equal(t, s.DefaultCatalog(), rs.Rows[0][0])
}

func TestDefaultUsesSequence_WholeName(t *testing.T) {
	assert.True(t, defaultUsesSequence(`nextval('_vibe_seq_t_id')`, "_vibe_seq_t_id"))
	assert.True(t, defaultUsesSequence(`nextval('"other"."main"."_vibe_seq_t_id"')`, "_vibe_seq_t_id"))
	assert.True(t, defaultUsesSequence(`NEXTVAL('_VIBE_SEQ_T_ID')`, "_vibe_seq_t_id"))
	assert.False(t, defaultUsesSequence(`nextval('_vibe_seq_t_id2')`, "_vibe_seq_t_id"), "seq must not match seq2")
	assert.False(t, defaultUsesSequence(`nextval('x__vibe_seq_t_id')`, "_vibe_seq_t_id"))
	assert.False(t, defaultUsesSequence(`'_vibe_seq_t_id'`, "_vibe_seq_t_id"))
}
