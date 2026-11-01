package lsp

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SandwichLabs/assay/internal/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const settle = 5 * time.Second

func newLive(t *testing.T, sess session.Session, opts ...LiveOption) *Live {
	t.Helper()
	l := NewLive(sess, append([]LiveOption{withLiveDebounce(5 * time.Millisecond)}, opts...)...)
	t.Cleanup(l.Close)
	return l
}

func settled(t *testing.T, l *Live) {
	t.Helper()
	require.Eventually(t, func() bool { return !l.Building() }, settle, 5*time.Millisecond)
}

// duckFile creates a DuckDB file with one table and returns its path.
func duckFile(t *testing.T, name, ddl string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name+".duckdb")
	f, err := sql.Open("duckdb", path)
	require.NoError(t, err)
	_, err = f.Exec(ddl)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return path
}

func complete(l *Live, src, conn string) CompleteResult {
	return l.Complete(src, len(src), conn)
}

func TestLive_BuildsConnectedOnOpen(t *testing.T) {
	sess := hubWith(t)
	later := duckFile(t, "later", "CREATE TABLE ledger (id INTEGER)")
	require.NoError(t, sess.Register(session.Spec{Name: "later", Path: later}))
	l := newLive(t, sess)
	settled(t, l)

	cat := l.Catalog()
	require.NotNil(t, cat.Conn("shop"))
	assert.NotNil(t, cat.Conn("shop").Schema("main").Relation("orders"))
	assert.False(t, cat.Conn("shop").Stale)
	require.NotNil(t, cat.Conn("later"))
	assert.True(t, cat.Conn("later").Stale)
	assert.Empty(t, cat.Conn("later").Schemas)

	res := complete(l, "SELECT * FROM ", "")
	var conns []string
	for _, it := range res.Items {
		if it.Kind == KindConnection {
			conns = append(conns, it.Label)
		}
	}
	assert.ElementsMatch(t, []string{"shop", "later"}, conns)
	assert.Equal(t, []string{"later"}, res.Stale)
	assert.Contains(t, cat.Functions, "date_trunc")
}

func TestLive_NeverConnects(t *testing.T) {
	sess := hubWith(t)
	later := duckFile(t, "later", "CREATE TABLE ledger (id INTEGER)")
	require.NoError(t, sess.Register(session.Spec{Name: "later", Path: later}))
	l := newLive(t, sess)
	settled(t, l)
	l.Invalidate("later")
	l.Invalidate("")
	settled(t, l)
	res := complete(l, "SELECT * FROM later.main.", "")
	assert.Empty(t, res.Items)
	st, ok := sess.Status("later")
	require.True(t, ok)
	assert.NotEqual(t, session.StatusOK, st.Kind, "completion must not connect")
	assert.Empty(t, l.Catalog().Conn("later").Schemas)
}

func TestLive_RebuildOnInvalidate(t *testing.T) {
	sess := hubWith(t)
	l := newLive(t, sess)
	settled(t, l)
	assert.Nil(t, l.Catalog().Conn("shop").Schema("main").Relation("new_t"))
	_, err := sess.DB().Exec(`CREATE TABLE shop.new_t (a INTEGER)`)
	require.NoError(t, err)
	l.Invalidate("shop")
	require.Eventually(t, func() bool {
		return l.Catalog().Conn("shop").Schema("main").Relation("new_t") != nil
	}, settle, 5*time.Millisecond)
}

func TestLive_StaleFallbackFromSnapshot(t *testing.T) {
	root := t.TempDir()
	otherPath := duckFile(t, "other", "CREATE TABLE ledger (id INTEGER, memo VARCHAR)")
	first := hubWith(t)
	require.NoError(t, first.Attach(context.Background(), session.Spec{Name: "other", Path: otherPath}))
	n, err := Snapshot(context.Background(), first, root)
	require.NoError(t, err)
	require.Equal(t, 2, n)

	// A new session where "other" is registered but not connected.
	sess := hubWith(t)
	require.NoError(t, sess.Register(session.Spec{Name: "other", Path: otherPath}))
	l := newLive(t, sess, WithLiveSnapshot(root, false))
	settled(t, l)
	other := l.Catalog().Conn("other")
	require.NotNil(t, other)
	assert.True(t, other.Stale)
	assert.NotNil(t, other.Schema("main").Relation("ledger"), "served from the snapshot")
	st, _ := sess.Status("other")
	assert.NotEqual(t, session.StatusOK, st.Kind)

	require.NoError(t, sess.Connect(context.Background(), "other"))
	l.Invalidate("other")
	require.Eventually(t, func() bool {
		c := l.Catalog().Conn("other")
		return c != nil && !c.Stale
	}, settle, 5*time.Millisecond)

	// Detach alone unregisters the connection: it is no longer offered.
	require.NoError(t, sess.Detach(context.Background(), "other"))
	l.Invalidate("")
	settled(t, l)
	assert.Nil(t, l.Catalog().Conn("other"), "an unregistered connection is not offered")
}

// Studio's DisconnectConnection is Detach then Register: the connection stays
// known, and what it had is still offered, marked stale.
func TestLive_DisconnectKeepsTablesStale(t *testing.T) {
	sess := hubWith(t)
	otherPath := duckFile(t, "other", "CREATE TABLE ledger (id INTEGER, memo VARCHAR)")
	require.NoError(t, sess.Attach(context.Background(), session.Spec{Name: "other", Path: otherPath}))
	l := newLive(t, sess)
	settled(t, l)
	live := l.Catalog().Conn("other")
	require.NotNil(t, live)
	require.False(t, live.Stale)
	require.NotNil(t, live.Schema("main").Relation("ledger"))

	require.NoError(t, sess.Detach(context.Background(), "other"))
	require.NoError(t, sess.Register(session.Spec{Name: "other", Path: otherPath}))

	// Before any rebuild: the built entry is marked stale because its status
	// is no longer ok.
	before := l.Catalog().Conn("other")
	require.NotNil(t, before)
	assert.True(t, before.Stale)
	assert.NotNil(t, before.Schema("main").Relation("ledger"))

	// After the rebuild drops the built entry, the saved copy takes over.
	l.Invalidate("other")
	settled(t, l)
	after := l.Catalog().Conn("other")
	require.NotNil(t, after)
	assert.True(t, after.Stale)
	assert.NotNil(t, after.Schema("main").Relation("ledger"), "the tables are kept, not forgotten")
	items := complete(l, "SELECT * FROM other.main.", "").Items
	require.Len(t, items, 1)
	assert.Equal(t, "ledger", items[0].Label)
}

func TestLive_CloseThenRebuildNeverWrites(t *testing.T) {
	root := t.TempDir()
	l := newLive(t, hubWith(t), WithLiveSnapshot(root, true))
	settled(t, l)
	require.Eventually(t, func() bool { _, err := os.Stat(SnapshotPath(root)); return err == nil }, settle, 5*time.Millisecond)
	require.NoError(t, os.Remove(SnapshotPath(root)))
	l.Close()
	l.rebuild(map[string]bool{"": true})
	l.writeSnapshot(&Catalog{Version: catalogVersion, Connections: []Connection{{Name: "shop"}}})
	_, err := os.Stat(SnapshotPath(root))
	assert.ErrorIs(t, err, os.ErrNotExist, "nothing is written after Close")
}

func TestLive_WritesSnapshotWhenOwner(t *testing.T) {
	root := t.TempDir()
	l := newLive(t, hubWith(t), WithLiveSnapshot(root, true))
	settled(t, l)
	require.Eventually(t, func() bool {
		c, err := Load(SnapshotPath(root))
		return err == nil && c.Conn("shop") != nil
	}, settle, 5*time.Millisecond)
	fi, err := os.Stat(SnapshotPath(root))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
	b, err := os.ReadFile(SnapshotPath(root))
	require.NoError(t, err)
	assert.NotContains(t, string(b), ".duckdb", "no path in the snapshot")

	ro := t.TempDir()
	l2 := newLive(t, hubWith(t), WithLiveSnapshot(ro, false))
	settled(t, l2)
	_, err = os.Stat(SnapshotPath(ro))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestLive_ConnectionDefaultsCatalog(t *testing.T) {
	sess := hubWith(t)
	otherPath := duckFile(t, "other", "CREATE TABLE ordinal_items (id INTEGER)")
	require.NoError(t, sess.Attach(context.Background(), session.Spec{Name: "other", Path: otherPath}))
	l := newLive(t, sess)
	settled(t, l)
	hubDefault := complete(l, "SELECT * FROM ord", "")
	var labels []string
	for _, it := range hubDefault.Items {
		labels = append(labels, it.Label)
	}
	assert.Contains(t, labels, "orders")
	assert.NotContains(t, labels, "ordinal_items")

	labels = nil
	for _, it := range complete(l, "SELECT * FROM ord", "other").Items {
		labels = append(labels, it.Label)
	}
	assert.Contains(t, labels, "ordinal_items")
	assert.NotContains(t, labels, "orders")
}

func TestLive_CloseStopsWorker(t *testing.T) {
	l := NewLive(hubWith(t), withLiveDebounce(time.Millisecond))
	start := time.Now()
	l.Close()
	assert.Less(t, time.Since(start), liveCloseWait)
	assert.NotPanics(t, func() { l.Invalidate("shop"); l.Invalidate("") })
	l.Close() // idempotent
}

func TestLive_CapsInput(t *testing.T) {
	l := newLive(t, hubWith(t))
	res := l.Complete(strings.Repeat("a", MaxCompleteBytes+1), 3, "")
	assert.Empty(t, res.Items)
}

func TestLive_UTF16Offsets(t *testing.T) {
	l := newLive(t, hubWith(t))
	settled(t, l)
	src := "SELECT 'café😀', tot FROM orders"
	// UTF-16 offset of the end of "tot": é is one unit, the emoji two.
	off := len([]rune("SELECT 'café")) + 2 + len([]rune("', tot"))
	res := l.Complete(src, off, "")
	var labels []string
	for _, it := range res.Items {
		labels = append(labels, it.Label)
	}
	assert.Contains(t, labels, "total")
	assert.Equal(t, off-3, res.From)
}

func TestLive_CompleteReportsIncomplete(t *testing.T) {
	sess := hubWith(t)
	var ddl strings.Builder
	for i := 0; i < maxItems+20; i++ {
		fmt.Fprintf(&ddl, "CREATE TABLE shop.main.t%04d (a INTEGER);", i)
	}
	_, err := sess.DB().Exec(ddl.String())
	require.NoError(t, err)
	l := newLive(t, sess)
	settled(t, l)
	res := complete(l, "SELECT * FROM t", "")
	assert.True(t, res.Incomplete)
	assert.Len(t, res.Items, maxItems)
	assert.False(t, complete(l, "SELECT * FROM orders", "").Incomplete)
}
