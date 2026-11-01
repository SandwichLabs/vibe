package agents

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/assay/internal/queries"
)

// queryFixture is the artifact fixture with the real file store behind the
// saved queries, so revs and ids behave as they do in a workspace.
func newQueryFixture(t *testing.T, mode SandboxMode, opts ...fixtureOpt) *artifactFixture {
	t.Helper()
	fx := newArtifactFixture(t, mode, opts...)
	acc, ok := fx.h.access.(*policyAccess)
	require.True(t, ok)
	acc.mockAccess.qs = queries.NewFileStore(fx.root)
	return fx
}

func (fx *artifactFixture) listQueries() []map[string]any {
	fx.t.Helper()
	resp := fx.call("list_queries", map[string]any{})
	require.Nil(fx.t, resp.Error, errMsg(resp))
	rows, ok := mapOf(fx.t, resp.Result)["queries"].([]map[string]any)
	require.True(fx.t, ok)
	return rows
}

func (fx *artifactFixture) saveQuery(p map[string]any) *Response {
	fx.t.Helper()
	return fx.call("save_query", p)
}

func TestSaveQuery_ReadWriteTierSavesAndListReturnsIdAndRev(t *testing.T) {
	fx := newQueryFixture(t, ModeReadWrite, withAsk(false))
	assert.Equal(t, "[]", marshal(t, mapOf(t, fx.call("list_queries", map[string]any{}).Result)["queries"]), "an empty list is [], not null")

	resp := fx.saveQuery(map[string]any{"title": "Named rows", "sql": "SELECT id, name FROM t"})
	require.Nil(t, resp.Error, errMsg(resp))
	res := mapOf(t, resp.Result)
	assert.Equal(t, "named-rows", res["id"])
	assert.NotEmpty(t, res["rev"])
	assert.Equal(t, true, res["created"])
	cols, _ := res["columns"].([]ArtifactColumn)
	require.Len(t, cols, 2)
	assert.Equal(t, "id", cols[0].Name)

	rows := fx.listQueries()
	require.Len(t, rows, 1)
	assert.Equal(t, "named-rows", rows[0]["id"])
	assert.Equal(t, res["rev"], rows[0]["rev"])
	assert.Equal(t, "Named rows", rows[0]["name"])
}

func marshal(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

func TestSaveQuery_RefusesWritesAndInvalidSQL(t *testing.T) {
	fx := newQueryFixture(t, ModeFull, withAsk(false))
	for name, sql := range map[string]string{
		"delete":      "DELETE FROM t",
		"syntax":      "SELEC 1",
		"bad column":  "SELECT nope FROM t",
		"two":         "SELECT 1; SELECT 2",
		"create":      "CREATE TABLE z (a INT)",
		"environment": "SELECT getenv('HOME')",
	} {
		resp := fx.saveQuery(map[string]any{"title": "Bad " + name, "sql": sql})
		require.NotNil(t, resp.Error, name)
		assert.Equal(t, ErrInvalidParams, resp.Error.Code, name)
	}
	assert.Empty(t, fx.listQueries(), "nothing was saved")

	resp := fx.saveQuery(map[string]any{"title": "Bad column", "sql": "SELECT nope FROM t"})
	assert.Contains(t, errMsg(resp), "nope")
	assert.Contains(t, errMsg(resp), "id", "the binder names the columns it knows")
}

func TestSaveQuery_DryRunSaysReadyAndWritesNothing(t *testing.T) {
	fx := newQueryFixture(t, ModeReadWrite, withAsk(true))
	resp := fx.saveQuery(map[string]any{"title": "Ready", "sql": "SELECT id FROM t"})
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrApprovalNeeded, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "valid and ready")
	assert.Contains(t, resp.Error.Message, "queries/")
	assert.Empty(t, fx.listQueries())
	_, err := os.Stat(filepath.Join(fx.root, "queries"))
	assert.True(t, os.IsNotExist(err) || dirEmpty(t, filepath.Join(fx.root, "queries")))

	resp = fx.saveQuery(map[string]any{"title": "Broken", "sql": "SELECT nope FROM t"})
	require.NotNil(t, resp.Error)
	assert.NotEqual(t, ErrApprovalNeeded, resp.Error.Code)
}

func dirEmpty(t *testing.T, dir string) bool {
	ents, err := os.ReadDir(dir)
	require.NoError(t, err)
	return len(ents) == 0
}

func TestSaveQuery_ReadOnlyTierIsRefused(t *testing.T) {
	fx := newQueryFixture(t, ModeReadOnly, withAsk(false))
	resp := fx.saveQuery(map[string]any{"title": "x", "sql": "SELECT 1"})
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrPermission, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "read-write")
}

func TestSaveQuery_DuplicateTitleIsAConflictNamingTheId(t *testing.T) {
	fx := newQueryFixture(t, ModeReadWrite, withAsk(false))
	require.Nil(t, fx.saveQuery(map[string]any{"title": "Since", "sql": "SELECT 1 AS a"}).Error)

	resp := fx.saveQuery(map[string]any{"title": "since", "sql": "SELECT 2 AS a"})
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrInvalidParams, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, `"since"`)
	assert.Contains(t, resp.Error.Message, "already exists")
	assert.Len(t, fx.listQueries(), 1, "no -2 fork")
}

func TestSaveQuery_UpdateNeedsIdAndRev(t *testing.T) {
	fx := newQueryFixture(t, ModeReadWrite, withAsk(false))
	first := mapOf(t, fx.saveQuery(map[string]any{"title": "Q", "sql": "SELECT 1 AS a"}).Result)
	id, rev := asString(t, first["id"]), asString(t, first["rev"])

	// no rev
	resp := fx.saveQuery(map[string]any{"id": id, "title": "Q", "sql": "SELECT 2 AS a"})
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "rev")
	// unknown id
	resp = fx.saveQuery(map[string]any{"id": "nope", "rev": rev, "title": "Q", "sql": "SELECT 2 AS a"})
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "no saved query")
	// right rev
	resp = fx.saveQuery(map[string]any{"id": id, "rev": rev, "title": "Q", "sql": "SELECT 2 AS a"})
	require.Nil(t, resp.Error, errMsg(resp))
	res := mapOf(t, resp.Result)
	assert.Equal(t, false, res["created"])
	assert.NotEqual(t, rev, res["rev"])
	rows := fx.listQueries()
	require.Len(t, rows, 1)
	assert.Equal(t, "SELECT 2 AS a", rows[0]["sql"])
	// the old rev is stale now
	resp = fx.saveQuery(map[string]any{"id": id, "rev": rev, "title": "Q", "sql": "SELECT 3 AS a"})
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "changed since")
	assert.Equal(t, "SELECT 2 AS a", fx.listQueries()[0]["sql"])
	// a title another query holds
	require.Nil(t, fx.saveQuery(map[string]any{"title": "Other", "sql": "SELECT 1 AS a"}).Error)
	resp = fx.saveQuery(map[string]any{"id": id, "rev": res["rev"], "title": "Other", "sql": "SELECT 3 AS a"})
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "already titled")
}

func TestSaveQuery_Parameters(t *testing.T) {
	fx := newQueryFixture(t, ModeReadWrite, withAsk(false))
	p := func(name, typ, def string) map[string]any {
		return map[string]any{"name": name, "type": typ, "default": def}
	}
	save := func(sql string, ps ...any) *Response {
		return fx.saveQuery(map[string]any{"title": "Param", "sql": sql, "parameters": ps})
	}
	// undeclared use
	resp := save("SELECT id FROM t WHERE id >= $min")
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "$min")
	assert.Contains(t, resp.Error.Message, "not a declared parameter")
	// declared but unused
	resp = save("SELECT id FROM t", p("min", "integer", "1"))
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "never uses")
	// bad type and bad default
	resp = save("SELECT id FROM t WHERE id >= $min", p("min", "int", "1"))
	require.NotNil(t, resp.Error)
	resp = save("SELECT id FROM t WHERE id >= $min", p("min", "integer", "abc"))
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "not an integer")
	assert.Empty(t, fx.listQueries())

	resp = save("SELECT id FROM t WHERE id >= $min", p("min", "integer", "1"))
	require.Nil(t, resp.Error, errMsg(resp))
	rows := fx.listQueries()
	require.Len(t, rows, 1)
	ps, _ := rows[0]["parameters"].([]queries.Param)
	require.Len(t, ps, 1)
	assert.Equal(t, "min", ps[0].Name)
	assert.Equal(t, "integer", ps[0].Type)
	assert.Equal(t, "1", ps[0].Default)
}

func TestRunSavedQuery_BindsParamsAndDefaults(t *testing.T) {
	fx := newQueryFixture(t, ModeReadOnly, withAsk(true))
	fx.exec("INSERT INTO t VALUES (2, 'Bo'), (3, 'Cy')")
	store := queries.NewFileStore(fx.root)
	q := &queries.Query{Title: "Min id", SQL: "SELECT id FROM t WHERE id >= $min ORDER BY id", Parameters: []queries.Param{{Name: "min", Type: "integer", Default: "2"}}}
	require.NoError(t, store.Create(context.Background(), q))

	run := func(p map[string]any) *Response { return fx.call("run_saved_query", p) }
	rowsOf := func(r *Response) [][]any {
		require.Nil(t, r.Error, errMsg(r))
		rows, ok := mapOf(t, r.Result)["rows"].([][]any)
		require.True(t, ok)
		return rows
	}
	assert.Len(t, rowsOf(run(map[string]any{"id": q.ID})), 2, "the default 2 gives ids 2 and 3")
	assert.Len(t, rowsOf(run(map[string]any{"id": q.ID, "params": map[string]any{"min": 3}})), 1)

	r := run(map[string]any{"id": q.ID, "params": map[string]any{"min": "3"}})
	require.NotNil(t, r.Error, "a string for an integer is refused, not converted")
	r = run(map[string]any{"id": q.ID, "params": map[string]any{"nope": 1}})
	require.NotNil(t, r.Error)
	assert.Contains(t, r.Error.Message, "no parameter named")
	r = run(map[string]any{"id": "missing"})
	require.NotNil(t, r.Error)
	assert.Contains(t, r.Error.Message, "no saved query")
}

func TestRunSavedQuery_RefusesAStoredWrite(t *testing.T) {
	fx := newQueryFixture(t, ModeFull, withAsk(false))
	// a hand-edited file the save-time vet never saw
	require.NoError(t, os.MkdirAll(filepath.Join(fx.root, "queries"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(fx.root, "queries", "evil.sql"), []byte("-- assay: {\"title\":\"evil\"}\nDELETE FROM t\n"), 0o644))
	resp := fx.call("run_saved_query", map[string]any{"id": "evil"})
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrPermission, resp.Error.Code)
	var n int
	require.NoError(t, fx.db.QueryRow("SELECT count(*) FROM t").Scan(&n))
	assert.Equal(t, 1, n, "the row is still there")
}

func TestRunSavedQuery_OnTheQuerysConnection(t *testing.T) {
	fx := newQueryFixture(t, ModeReadOnly, withAsk(true))
	resp := fx.call("save_query", map[string]any{"title": "Other orders", "sql": "SELECT id, total FROM orders ORDER BY id", "connection": "other"})
	require.NotNil(t, resp.Error, "read-only cannot save")
	fx.h.SetFilePolicy(ModeReadWrite, false)
	resp = fx.saveQuery(map[string]any{"title": "Other orders", "sql": "SELECT id, total FROM orders ORDER BY id", "connection": "other"})
	require.Nil(t, resp.Error, errMsg(resp))
	fx.h.SetFilePolicy(ModeReadOnly, true)
	resp = fx.call("run_saved_query", map[string]any{"id": "other-orders"})
	require.Nil(t, resp.Error, errMsg(resp))
	assert.Len(t, mapOf(t, resp.Result)["rows"], 2)
}

func TestBuildQuery_TypesLiteralsByColumnAndSavesThroughTheSamePath(t *testing.T) {
	fx := newQueryFixture(t, ModeReadWrite, withAsk(false))
	resp := fx.call("build_query", map[string]any{"name": "Over 15", "table": "t", "conditions": []any{
		map[string]any{"column": "id", "op": "gt", "value": "15"},
		map[string]any{"column": "name", "op": "neq", "value": "15"},
	}})
	require.Nil(t, resp.Error, errMsg(resp))
	sql, _ := mapOf(t, resp.Result)["sql"].(string)
	assert.Contains(t, sql, `"id" > 15`)
	assert.Contains(t, sql, `"name" <> '15'`, "a text column keeps the string")

	resp = fx.call("build_query", map[string]any{"name": "Not a number", "table": "t", "conditions": []any{
		map[string]any{"column": "id", "op": "gt", "value": "abc"},
	}})
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "not a number")

	resp = fx.call("build_query", map[string]any{"name": "over 15", "table": "t"})
	require.NotNil(t, resp.Error, "the title is taken")
	assert.Contains(t, resp.Error.Message, "already exists")

	// the saved query runs by id
	run := fx.call("run_saved_query", map[string]any{"id": "over-15"})
	require.Nil(t, run.Error, errMsg(run))
}

func TestBuildQuery_KeepsEveryDigitOfALargeNumber(t *testing.T) {
	fx := newQueryFixture(t, ModeReadWrite, withAsk(false))
	fx.exec("CREATE TABLE big (d DECIMAL(20,2), h HUGEINT); INSERT INTO big VALUES (12345678901234567.89, 170141183460469231731687303715884105727)")
	resp := fx.call("build_query", map[string]any{"name": "Exact", "table": "big", "conditions": []any{
		map[string]any{"column": "d", "op": "eq", "value": "12345678901234567.89"},
		map[string]any{"column": "h", "op": "eq", "value": "170141183460469231731687303715884105727"},
	}})
	require.Nil(t, resp.Error, errMsg(resp))
	sql, _ := mapOf(t, resp.Result)["sql"].(string)
	assert.Contains(t, sql, `"d" = 12345678901234567.89`)
	assert.Contains(t, sql, `"h" = 170141183460469231731687303715884105727`)
	run := fx.call("run_saved_query", map[string]any{"id": "exact"})
	require.Nil(t, run.Error, errMsg(run))
	rows, ok := mapOf(t, run.Result)["rows"].([][]any)
	require.True(t, ok)
	assert.Len(t, rows, 1, "the exact row matches")
}

func TestRunSavedQuery_StoredWriteIsRefusedInEveryTierWithoutAskingAnyone(t *testing.T) {
	for _, mode := range []SandboxMode{ModeReadOnly, ModeReadWrite, ModeFull} {
		fx := newQueryFixture(t, mode, withAsk(true), withApprover())
		require.NoError(t, os.MkdirAll(filepath.Join(fx.root, "queries"), 0o755))
		for id, body := range map[string]string{"w": "DELETE FROM t", "d": "DROP TABLE t"} {
			require.NoError(t, os.WriteFile(filepath.Join(fx.root, "queries", id+".sql"), []byte("-- assay: {\"title\":\""+id+"\"}\n"+body+"\n"), 0o644))
			resp := fx.call("run_saved_query", map[string]any{"id": id})
			require.NotNil(t, resp.Error, string(mode)+id)
			assert.Contains(t, resp.Error.Message, "runs reads only", string(mode)+id)
			assert.NotContains(t, resp.Error.Message, "needs the read-write tier")
		}
		assert.Equal(t, 0, fx.approver.count(), "the approver is never called: "+string(mode))
		var n int
		require.NoError(t, fx.db.QueryRow("SELECT count(*) FROM t").Scan(&n))
		assert.Equal(t, 1, n)
	}
}

func TestSaveQuery_DuplicateNamesTheToolCalled(t *testing.T) {
	fx := newQueryFixture(t, ModeReadWrite, withAsk(false))
	require.Nil(t, fx.call("build_query", map[string]any{"name": "Dup", "table": "t"}).Error)
	resp := fx.call("build_query", map[string]any{"name": "Dup", "table": "t"})
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "send build_query with id")
}
