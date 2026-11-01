package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/slaw/internal/artifacts"
	"github.com/SandwichLabs/slaw/internal/engine"
	"github.com/SandwichLabs/slaw/internal/session"
	"github.com/SandwichLabs/slaw/internal/skills"
)

// artifactView is a view over the manifests below.
const artifactView = `{"title":"T","components":[{"id":"t","type":"table","query":"rows"}]}`

// artifactManifest builds a manifest map the way a model sends one: numbers
// for numeric defaults, queries as objects.
func artifactManifest(queries ...map[string]any) map[string]any {
	qs := make([]any, len(queries))
	for i, q := range queries {
		qs[i] = q
	}
	return map[string]any{
		"title": "Test",
		"parameters": []any{
			map[string]any{"name": "min_id", "type": "integer", "default": float64(0)},
			map[string]any{"name": "who", "type": "text", "default": ""},
		},
		"queries": qs,
	}
}

func rowsQuery(sqlText string) map[string]any {
	return map[string]any{"name": "rows", "sql": sqlText}
}

const okSQL = "SELECT id, name FROM t WHERE id >= $min_id AND ($who = '' OR name = $who)"

func viewMap(t *testing.T, raw string) map[string]any {
	t.Helper()
	var v map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &v))
	return v
}

// artifactFixture is the policy fixture with a file-backed artifact store.
type artifactFixture struct {
	*policyFixture
	root  string
	store *artifacts.FileStore
}

func newArtifactFixture(t *testing.T, mode SandboxMode, opts ...fixtureOpt) *artifactFixture {
	t.Helper()
	fx := newPolicyFixture(t, mode, opts...)
	root := filepath.Join(fx.dir, "ws")
	store := artifacts.NewFileStore(root)
	setArtifactStore(t, fx.h, store)
	return &artifactFixture{policyFixture: fx, root: root, store: store}
}

// useStore swaps the store the handler's workspace reads and writes.
func (fx *artifactFixture) useStore(s artifacts.Store) { setArtifactStore(fx.t, fx.h, s) }

func setArtifactStore(t *testing.T, h *Handler, s artifacts.Store) {
	t.Helper()
	acc, ok := h.access.(*policyAccess)
	require.True(t, ok)
	acc.mockAccess.ars = s
}

func asString(t *testing.T, v any) string {
	t.Helper()
	s, ok := v.(string)
	require.True(t, ok, "expected a string, got %T", v)
	return s
}

func asSlice(t *testing.T, v any) []any {
	t.Helper()
	out, ok := v.([]any)
	require.True(t, ok, "expected a list, got %T", v)
	return out
}

func asRows(t *testing.T, v any) []map[string]any {
	t.Helper()
	rows, ok := v.([]map[string]any)
	require.True(t, ok, "expected rows, got %T", v)
	return rows
}

func (fx *artifactFixture) write(name string, manifest map[string]any, view string, rev string) *Response {
	fx.t.Helper()
	p := map[string]any{"name": name, "manifest": manifest, "view": viewMap(fx.t, view)}
	if rev != "" {
		p["rev"] = rev
	}
	return fx.call("write_artifact", p)
}

func (fx *artifactFixture) mustWrite(name string, manifest map[string]any) string {
	fx.t.Helper()
	resp := fx.write(name, manifest, artifactView, "")
	require.Nil(fx.t, resp.Error, errMsg(resp))
	return asString(fx.t, mapOf(fx.t, resp.Result)["rev"])
}

func (fx *artifactFixture) run(name, query string, values map[string]any) (int, bool, error) {
	rs, truncated, err := fx.h.RunArtifactQuery(context.Background(), name, query, values)
	if err != nil {
		return 0, false, err
	}
	return len(rs.Rows), truncated, nil
}

func TestWriteArtifact_LandsAsFiles(t *testing.T) {
	fx := newArtifactFixture(t, ModeReadWrite)
	resp := fx.write("sales", artifactManifest(rowsQuery(okSQL)), artifactView, "")
	require.Nil(t, resp.Error, errMsg(resp))
	res := mapOf(t, resp.Result)
	assert.Equal(t, true, res["saved"])
	assert.NotEmpty(t, res["rev"])

	manifest, err := os.ReadFile(filepath.Join(fx.root, "artifacts", "sales", "artifact.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(manifest), "min_id")
	assert.Contains(t, string(manifest), "default: 0\n", "an integer default is written as an integer")
	view, err := os.ReadFile(filepath.Join(fx.root, "artifacts", "sales", "view.a2ui.json"))
	require.NoError(t, err)
	assert.Contains(t, string(view), `"query": "rows"`)

	// And the stored pair runs.
	n, _, err := fx.run("sales", "rows", nil)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

func TestWriteArtifact_RefusesWhatTheVetRefuses(t *testing.T) {
	cases := []struct{ name, sql, want string }{
		{"write statement", "DELETE FROM t WHERE id = $min_id", "agent's read policy"},
		{"parameter as a reader path", "SELECT * FROM read_csv($who)", "cannot be an argument of read_csv"},
		{"parameter as a reader list", "SELECT * FROM read_parquet(['a.parquet', $who])", "cannot be an argument of read_parquet"},
		{"parameter in query()", "SELECT * FROM query($who)", "cannot be an argument of query"},
		{"parameter in query_table()", "SELECT * FROM query_table($who)", "cannot be an argument of query_table"},
		{"placeholder in a string", "SELECT id FROM t WHERE name = '$who'", "inside a string literal"},
		{"placeholder in a quoted name", `SELECT id AS "$who" FROM t WHERE id > $min_id`, "inside a string literal"},
		{"placeholder in a dollar-quoted body", "SELECT $$ $who $$ FROM t WHERE id > $min_id", "inside a string literal"},
		{"placeholder in a comment", "SELECT id FROM t -- $who\nWHERE id > $min_id", "inside a string literal"},
		{"escape string", "SELECT E'$who' FROM t WHERE id > $min_id", "inside a string literal"},
		{"bare question mark", "SELECT id FROM t WHERE id > ?", "bare ?"},
		{"file outside data", "SELECT * FROM read_csv('/etc/passwd')", "agent's read policy"},
		{"secret function", "SELECT * FROM duckdb_secrets()", "agent's read policy"},
		{"two statements", "SELECT 1; SELECT 2", "exactly one statement"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fx := newArtifactFixture(t, ModeFull)
			resp := fx.write("bad", artifactManifest(rowsQuery(c.sql)), artifactView, "")
			require.NotNil(t, resp.Error, "expected a refusal")
			assert.Contains(t, resp.Error.Message, c.want)
			assert.Contains(t, resp.Error.Message, `query "rows"`)
			list, err := fx.store.List(context.Background())
			require.NoError(t, err)
			assert.Empty(t, list, "a refused write must leave no files")
		})
	}
}

func TestWriteArtifact_RefusesAPositionalParameter(t *testing.T) {
	fx := newArtifactFixture(t, ModeFull)
	resp := fx.write("bad", artifactManifest(rowsQuery("SELECT id FROM t WHERE id > $1")), artifactView, "")
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "$1")
}

func TestWriteArtifact_ConnectionRules(t *testing.T) {
	fx := newArtifactFixture(t, ModeFull, withExtraConns(
		session.ConnectionInfo{Name: "lake", Type: "s3", Kind: session.StatusOK, Remote: true, Catalogless: true, Scope: "s3://lake/raw/"},
	))
	q := func(conn string) map[string]any {
		return map[string]any{"name": "rows", "connection": conn, "sql": "SELECT id, total FROM orders"}
	}
	resp := fx.write("a", artifactManifest(q("nope")), artifactView, "")
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, `no connection named "nope"`)

	resp = fx.write("a", artifactManifest(q("lake")), artifactView, "")
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "storage connection with no catalog")

	// A real connection vets on that connection: orders exists only in "other".
	resp = fx.write("a", artifactManifest(q("other")), artifactView, "")
	require.Nil(t, resp.Error, errMsg(resp))
	n, _, err := fx.run("a", "rows", nil)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	// The same statement on the default connection does not resolve there.
	resp = fx.write("b", artifactManifest(rowsQuery("SELECT id, total FROM orders")), artifactView, "")
	require.NotNil(t, resp.Error, "vetted on the connection it will run on")
}

func TestWriteArtifact_StructuralRefusals(t *testing.T) {
	fx := newArtifactFixture(t, ModeFull)
	good := func() map[string]any { return artifactManifest(rowsQuery(okSQL)) }
	withKey := good()
	withKey["password"] = "hunter2"
	noDefault := good()
	noDefault["parameters"] = []any{map[string]any{"name": "who", "type": "text"}}
	undeclared := artifactManifest(rowsQuery("SELECT id FROM t WHERE id > $nope"))

	for name, p := range map[string]map[string]any{
		"unknown manifest key":  {"name": "x", "manifest": withKey, "view": viewMap(t, artifactView)},
		"manifest not object":   {"name": "x", "manifest": "title: x", "view": viewMap(t, artifactView)},
		"view not object":       {"name": "x", "manifest": good(), "view": "{}"},
		"parameter no default":  {"name": "x", "manifest": noDefault, "view": viewMap(t, artifactView)},
		"undeclared parameter":  {"name": "x", "manifest": undeclared, "view": viewMap(t, artifactView)},
		"view with sql":         {"name": "x", "manifest": good(), "view": viewMap(t, `{"title":"T","components":[{"id":"a","type":"table","sql":"SELECT 1"}]}`)},
		"view names no query":   {"name": "x", "manifest": good(), "view": viewMap(t, `{"title":"T","components":[{"id":"a","type":"table","query":"gone"}]}`)},
		"name with a slash":     {"name": "a/b", "manifest": good(), "view": viewMap(t, artifactView)},
		"name with dots":        {"name": "../x", "manifest": good(), "view": viewMap(t, artifactView)},
		"upper case name":       {"name": "Sales", "manifest": good(), "view": viewMap(t, artifactView)},
		"windows device name":   {"name": "con", "manifest": good(), "view": viewMap(t, artifactView)},
		"missing view":          {"name": "x", "manifest": good()},
		"integer default float": {"name": "x", "manifest": map[string]any{"title": "T", "parameters": []any{map[string]any{"name": "n", "type": "integer", "default": 1.5}}, "queries": []any{rowsQuery("SELECT $n AS n")}}, "view": viewMap(t, artifactView)},
	} {
		t.Run(name, func(t *testing.T) {
			resp := fx.call("write_artifact", p)
			require.NotNil(t, resp.Error)
			list, err := fx.store.List(context.Background())
			require.NoError(t, err)
			assert.Empty(t, list)
		})
	}
	entries, err := os.ReadDir(fx.root)
	if err == nil {
		for _, e := range entries {
			assert.NotEqual(t, "x", e.Name())
		}
	}
}

func TestWriteArtifact_ReadOnlyTierRefuses(t *testing.T) {
	fx := newArtifactFixture(t, ModeReadOnly)
	resp := fx.write("sales", artifactManifest(rowsQuery(okSQL)), artifactView, "")
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrPermission, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "not allowed in read-only mode")
	list, err := fx.store.List(context.Background())
	require.NoError(t, err)
	assert.Empty(t, list)

	// The reading tools stay available.
	assert.Nil(t, fx.call("list_artifacts", map[string]any{}).Error)
}

func TestWriteArtifact_AsksBeforeChangesLikeOtherContentTools(t *testing.T) {
	fx := newArtifactFixture(t, ModeReadWrite, withAsk(true), withApprover())
	resp := fx.write("sales", artifactManifest(rowsQuery(okSQL)), artifactView, "")
	require.Nil(t, resp.Error, errMsg(resp))
	require.Equal(t, 1, fx.approver.count())
	assert.Equal(t, "Save data artifact sales", fx.approver.asked[0].Summary)
	assert.Contains(t, fx.approver.asked[0].SQL, okSQL)
	rev := asString(t, mapOf(t, resp.Result)["rev"])
	resp = fx.write("sales", artifactManifest(rowsQuery(okSQL)), artifactView, rev)
	require.Nil(t, resp.Error, errMsg(resp))
	assert.Equal(t, "Replace data artifact sales", fx.approver.asked[1].Summary)

	// Headless (no approver): refused, nothing written.
	fx2 := newArtifactFixture(t, ModeReadWrite, withAsk(true))
	resp = fx2.write("sales", artifactManifest(rowsQuery(okSQL)), artifactView, "")
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrApprovalNeeded, resp.Error.Code)
}

// The agent must not replace a version it never read, even though the store
// has seen that version (a listing, Studio, the person's own save).
func TestWriteArtifact_NeverOverwritesWhatItDidNotRead(t *testing.T) {
	ctx := context.Background()
	for _, kind := range []string{"file", "mock"} {
		t.Run(kind, func(t *testing.T) {
			fx := newArtifactFixture(t, ModeReadWrite)
			var store artifacts.Store = fx.store
			if kind == "mock" {
				store = &artifacts.Mock{}
				fx.useStore(store)
			}
			// The person's version, written and read through the same store.
			persons := artifacts.Artifact{Name: "sales", View: []byte(artifactView)}
			persons.Manifest = *mustManifest(t, artifactManifest(rowsQuery("SELECT id, name FROM t WHERE id = $min_id")))
			require.NoError(t, store.Put(ctx, &persons, artifacts.WithRev("")))
			_, err := store.Get(ctx, "sales")
			require.NoError(t, err)
			_, err = store.List(ctx)
			require.NoError(t, err)

			// No rev: create-only, so an existing name is refused and unchanged.
			resp := fx.write("sales", artifactManifest(rowsQuery(okSQL)), artifactView, "")
			require.NotNil(t, resp.Error)
			assert.Contains(t, resp.Error.Message, "already exists")
			still, err := store.Get(ctx, "sales")
			require.NoError(t, err)
			assert.Equal(t, persons.Rev, still.Rev)
			assert.Contains(t, still.Manifest.Queries[0].SQL, "id = $min_id")

			// A rev that is not the current one is refused too.
			resp = fx.write("sales", artifactManifest(rowsQuery(okSQL)), artifactView, "0123456789abcdef0123456789abcdef")
			require.NotNil(t, resp.Error)
			assert.Contains(t, resp.Error.Message, "changed since")

			// The rev get_artifact returns replaces it.
			got := fx.call("get_artifact", map[string]any{"name": "sales"})
			require.Nil(t, got.Error, errMsg(got))
			rev := asString(t, mapOf(t, got.Result)["rev"])
			resp = fx.write("sales", artifactManifest(rowsQuery(okSQL)), artifactView, rev)
			require.Nil(t, resp.Error, errMsg(resp))
			newRev := asString(t, mapOf(t, resp.Result)["rev"])
			assert.NotEqual(t, rev, newRev)

			// The person edits meanwhile: the agent's next write with the old rev conflicts.
			edited := artifacts.Artifact{Name: "sales", Manifest: *mustManifest(t, artifactManifest(rowsQuery("SELECT id, name FROM t"))), View: []byte(artifactView)}
			require.NoError(t, store.Put(ctx, &edited, artifacts.WithRev(newRev)))
			resp = fx.write("sales", artifactManifest(rowsQuery(okSQL)), artifactView, newRev)
			require.NotNil(t, resp.Error)
			assert.Contains(t, resp.Error.Message, "changed since")
			final, err := store.Get(ctx, "sales")
			require.NoError(t, err)
			assert.Equal(t, edited.Rev, final.Rev)
		})
	}
}

func mustManifest(t *testing.T, m map[string]any) *artifacts.Manifest {
	t.Helper()
	out, _, err := manifestFromParam(m)
	require.NoError(t, err)
	require.NoError(t, artifacts.Validate(out))
	return out
}

func TestGetArtifact_ReturnsDefinitionsNeverResults(t *testing.T) {
	fx := newArtifactFixture(t, ModeReadOnly)
	long := "SELECT id, name FROM t WHERE id >= $min_id /* " + strings.Repeat("x", 5000) + " */"
	fxw := newArtifactFixture(t, ModeReadWrite)
	fx.useStore(fxw.store)
	fxw.mustWrite("sales", artifactManifest(rowsQuery(long)))

	resp := fx.call("get_artifact", map[string]any{"name": "sales"})
	require.Nil(t, resp.Error, errMsg(resp))
	res := mapOf(t, resp.Result)
	assert.NotEmpty(t, res["rev"])
	for _, k := range []string{"rows", "columns", "result", "results", "data"} {
		assert.NotContains(t, res, k)
	}
	manifest := mapOf(t, res["manifest"])
	assert.Equal(t, "Test", manifest["title"])
	q := asRows(t, manifest["queries"])[0]
	assert.Equal(t, true, q["sql_omitted"])
	assert.Nil(t, q["sql"])
	assert.Greater(t, q["sql_length"], notebookCellSourceBudgetRunes)
	raw, err := json.Marshal(res)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "SELECT id, name FROM t", "no part of the long SQL is returned")
	assert.Equal(t, []string{"min_id"}, q["uses"])
	assert.Equal(t, "T", mapOf(t, res["view"])["title"])
}

func TestGetArtifact_CannotReadOutsideArtifacts(t *testing.T) {
	fx := newArtifactFixture(t, ModeReadOnly)
	// A real notebook-ish file and a symlinked artifact folder pointing outside.
	require.NoError(t, os.MkdirAll(filepath.Join(fx.root, "notebooks"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(fx.root, "notebooks", "x"), []byte("SECRET"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(fx.root, "artifacts"), 0o755))
	// A valid artifact outside the workspace, linked in.
	outside := filepath.Join(fx.outside, "evil")
	other := artifacts.NewFileStore(fx.outside)
	a := artifacts.Artifact{Name: "evil", View: []byte(artifactView), Manifest: *mustManifest(t, artifactManifest(rowsQuery(okSQL)))}
	require.NoError(t, other.Put(context.Background(), &a, artifacts.WithRev("")))
	require.NoError(t, os.Symlink(filepath.Join(fx.outside, "artifacts", "evil"), filepath.Join(fx.root, "artifacts", "linked")))
	_ = outside

	for _, name := range []string{"../notebooks/x", "..", "a/b", "/etc/passwd", "notebooks", `..\x`, "", "linked", "LINKED", "con"} {
		resp := fx.call("get_artifact", map[string]any{"name": name})
		require.NotNil(t, resp.Error, "name %q", name)
		assert.NotContains(t, resp.Error.Message, "SECRET")
		assert.NotContains(t, resp.Error.Message, fx.root, "no absolute path in %q", resp.Error.Message)
		assert.NotContains(t, resp.Error.Message, fx.outside)
	}
	// The listing does not follow the symlinked folder out of the workspace.
	list := fx.call("list_artifacts", map[string]any{})
	require.Nil(t, list.Error)
	assert.Empty(t, mapOf(t, list.Result)["artifacts"])
}

func TestListArtifacts_ShowsBrokenOnesWithTheirError(t *testing.T) {
	fx := newArtifactFixture(t, ModeReadOnly)
	fxw := newArtifactFixture(t, ModeReadWrite)
	fx.useStore(fxw.store)
	fxw.mustWrite("good", artifactManifest(rowsQuery(okSQL)))
	broken := filepath.Join(fxw.root, "artifacts", "broken")
	require.NoError(t, os.MkdirAll(broken, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(broken, "artifact.yaml"), []byte("title: x\nqueries: [oops"), 0o644))

	resp := fx.call("list_artifacts", map[string]any{})
	require.Nil(t, resp.Error)
	rows := asRows(t, mapOf(t, resp.Result)["artifacts"])
	require.Len(t, rows, 2)
	assert.Equal(t, "broken", rows[0]["name"])
	assert.NotEmpty(t, rows[0]["error"])
	assert.Equal(t, "good", rows[1]["name"])
	assert.Equal(t, 1, rows[1]["queries"])
	assert.Equal(t, 2, rows[1]["parameters"])
	assert.NotEmpty(t, rows[1]["rev"])
}

func TestArtifactTools_ErrorsNeverLeakPathsOrCredentials(t *testing.T) {
	secret := "open /home/zac/secret/ws/artifacts/x/artifact.yaml: permission denied (postgres://u:hunter2@db.internal/prod, password=hunter2)"
	for _, method := range []string{"list_artifacts", "get_artifact", "write_artifact"} {
		fx := newArtifactFixture(t, ModeReadWrite)
		fx.useStore(&artifacts.Mock{Err: fmt.Errorf("%s", secret)})
		params := map[string]any{"name": "x", "manifest": artifactManifest(rowsQuery(okSQL)), "view": viewMap(t, artifactView)}
		resp := fx.call(method, params)
		require.NotNil(t, resp.Error, method)
		for _, leak := range []string{"/home/zac", "hunter2", "db.internal", "permission denied"} {
			assert.NotContains(t, resp.Error.Message, leak, method)
		}
	}
	// A structural error from the store passes through with any path scrubbed.
	assert.NotContains(t, scrubStoreText("failed to read /home/zac/ws/x: boom"), "/home/zac")
	assert.NotContains(t, scrubStoreText(`stat "C:\Users\zac\ws": boom`), `C:\Users`)
	assert.Contains(t, scrubStoreText("artifact.yaml is missing"), "artifact.yaml is missing")
}

func TestRunArtifactQuery_ValuesAreBoundNeverSQL(t *testing.T) {
	fx := newArtifactFixture(t, ModeReadWrite)
	fx.mustWrite("sales", artifactManifest(rowsQuery(okSQL)))

	n, _, err := fx.run("sales", "rows", nil)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	n, _, err = fx.run("sales", "rows", map[string]any{"who": "Ada", "min_id": float64(1)})
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	for _, hostile := range []string{"'; DROP TABLE t; --", "x' OR '1'='1", "Ada' --", "$$; DROP TABLE t; $$", "${HOME}"} {
		n, _, err = fx.run("sales", "rows", map[string]any{"who": hostile})
		require.NoError(t, err, hostile)
		assert.Equal(t, 0, n, "%q must match nothing", hostile)
	}
	assert.True(t, tableExists(t, fx.db, "t"), "nothing was dropped")
	assert.Equal(t, 1, fx.count("SELECT count(*) FROM t"))

	// A value of the wrong shape is refused, not coerced into SQL.
	_, _, err = fx.run("sales", "rows", map[string]any{"min_id": "0 OR 1=1"})
	require.Error(t, err)
	// Unknown names are refused.
	_, _, err = fx.run("sales", "nope", nil)
	require.ErrorContains(t, err, "no query named")
	_, _, err = fx.run("sales", "rows", map[string]any{"nope": 1})
	require.ErrorContains(t, err, "no parameter named")
	_, _, err = fx.run("missing", "rows", nil)
	require.Error(t, err)
	_, _, err = fx.run("../notebooks/x", "rows", nil)
	require.Error(t, err)
}

func TestRunArtifactQuery_HugeLimitStillCapped(t *testing.T) {
	fx := newArtifactFixture(t, ModeReadOnly)
	m := map[string]any{
		"title":      "Big",
		"parameters": []any{map[string]any{"name": "n", "type": "integer", "default": float64(5)}},
		"queries":    []any{rowsQuery("SELECT i FROM range(1000000) AS r(i) LIMIT $n")},
	}
	fxw := newArtifactFixture(t, ModeReadWrite)
	fx.useStore(fxw.store)
	fxw.mustWrite("big", m)

	n, truncated, err := fx.run("big", "rows", nil)
	require.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.False(t, truncated)
	n, truncated, err = fx.run("big", "rows", map[string]any{"n": float64(1 << 52)})
	require.NoError(t, err)
	assert.Equal(t, artifactQueryRowCap, n)
	assert.True(t, truncated)
}

// A file can arrive by git, an editor or a bundle without passing the vet; the
// run repeats the checks.
func TestRunArtifactQuery_ReChecksWhatWasEditedOnDisk(t *testing.T) {
	cases := []struct{ name, sql, want string }{
		{"write statement", "DELETE FROM t WHERE id = $min_id", ErrReadOnlyBindingDenied.Error()},
		{"secret function", "SELECT * FROM duckdb_secrets()", ErrReadOnlyBindingDenied.Error()},
		{"file outside data", "SELECT * FROM read_csv('/etc/passwd')", ErrReadOnlyBindingDenied.Error()},
		{"placeholder in a literal", "SELECT '$who' AS x FROM t WHERE id > $min_id", "inside a string literal"},
		{"placeholder in a comment", "SELECT id FROM t -- $who\nWHERE id > $min_id", "inside a string literal"},
		{"parameter as reader path", "SELECT * FROM read_csv($who)", "cannot be an argument of read_csv"},
		{"parameter in query()", "SELECT * FROM query($who)", "cannot be an argument of query"},
		{"bare question mark", "SELECT id FROM t WHERE id > ?", "bare ?"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fx := newArtifactFixture(t, ModeFull)
			fx.mustWrite("sales", artifactManifest(rowsQuery(okSQL)))
			path := filepath.Join(fx.root, "artifacts", "sales", "artifact.yaml")
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			edited := strings.Replace(string(data), okSQL, strings.ReplaceAll(c.sql, "\n", " "), 1)
			if strings.Contains(c.sql, "\n") {
				edited = strings.Replace(string(data), okSQL, "SELECT id FROM t -- $who\n      WHERE id > $min_id", 1)
			}
			require.NotEqual(t, string(data), edited)
			require.NoError(t, os.WriteFile(path, []byte(edited), 0o644))

			_, _, err = fx.run("sales", "rows", nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.want)
			assert.Equal(t, 1, fx.count("SELECT count(*) FROM t"), "nothing was changed")
		})
	}
}

// The connection is resolved the same way at save and at run, and re-checked
// at run, so an edit that names another connection is vetted as that one.
func TestRunArtifactQuery_ReChecksTheConnection(t *testing.T) {
	fx := newArtifactFixture(t, ModeFull, withExtraConns(
		session.ConnectionInfo{Name: "lake", Type: "s3", Kind: session.StatusOK, Remote: true, Catalogless: true, Scope: "s3://lake/raw/"},
	))
	fx.mustWrite("sales", artifactManifest(map[string]any{"name": "rows", "connection": "other", "sql": "SELECT id, total FROM orders"}))
	path := filepath.Join(fx.root, "artifacts", "sales", "artifact.yaml")
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	for conn, want := range map[string]string{"gone": `no connection named "gone"`, "lake": "storage connection with no catalog", "ro": "ledger"} {
		edited := strings.Replace(string(data), "connection: other", "connection: "+conn, 1)
		require.NoError(t, os.WriteFile(path, []byte(edited), 0o644))
		_, _, err = fx.run("sales", "rows", nil)
		require.Error(t, err, conn)
		if conn != "ro" { // "ro" is real: the statement just does not resolve there
			assert.Contains(t, err.Error(), want)
		}
	}
}

func TestArtifactTools_AreInTheSurfaceButNotTaskMode(t *testing.T) {
	want := map[string]string{"list_artifacts": "introspect", "get_artifact": "introspect", "write_artifact": "content"}
	for name, cat := range want {
		td := GetTool(name)
		require.NotNil(t, td, name)
		assert.Equal(t, cat, td.Category)
		assert.True(t, IsModeVisible(name, ModeMCP), name)
		assert.True(t, IsModeVisible(name, ModeChat), name)
		assert.False(t, IsModeVisible(name, ModeTask), name)
		assert.False(t, TaskAllowedToolNames[name], name)
	}
	for _, td := range ListToolsForMode(ModeTask) {
		assert.NotContains(t, td.Name, "artifact")
	}
	// No tool runs an artifact for the agent: its results never reach it.
	for _, td := range ListTools() {
		if strings.Contains(td.Name, "artifact") {
			_, known := want[td.Name]
			assert.True(t, known, "unexpected artifact tool %q", td.Name)
		}
		assert.NotRegexp(t, `run_artifact|artifact_query|artifact_result`, td.Name)
	}
	assert.Contains(t, GetTool("write_artifact").Description, "data-artifacts")
}

func TestCheckArtifactSQLShape(t *testing.T) {
	ok := []string{
		"SELECT * FROM t WHERE a = $x",
		"SELECT * FROM t WHERE a BETWEEN $from AND $to AND b = $from",
		"SELECT '?' AS q, 'a $ b' FROM t WHERE a = $x", // a literal $ without a name is not a placeholder
		"SELECT * FROM read_csv('/data/x.csv') WHERE a = $x",
		"SELECT json_extract(j, '$.a') FROM t WHERE b = $x",
		"SELECT * FROM t WHERE a = $x::DATE - INTERVAL 1 DAY",
		"SELECT * FROM t -- no params in this comment\nWHERE a = $x",
		"SELECT a FROM t WHERE b IN ($x, $y)",
	}
	for _, s := range ok {
		assert.NoError(t, checkArtifactSQLShape(s), s)
	}
	bad := []string{
		"SELECT * FROM t WHERE a = ?",
		"SELECT * FROM t WHERE a = $1",
		"SELECT * FROM t WHERE a = $",
		"SELECT * FROM t WHERE a = $ x",
		"SELECT * FROM t WHERE a = '$x'",
		"SELECT * FROM t WHERE a = $x AND b = '$x'",
		`SELECT "$x" FROM t`,
		"SELECT $$ $x $$",
		"SELECT $tag$ $x $tag$",
		"SELECT * FROM t /* $x */ WHERE a = $x",
		"SELECT * FROM t WHERE a = $é",
		"SELECT * FROM read_json($x)",
		"SELECT * FROM READ_PARQUET(['a', $x])",
		`SELECT * FROM "read_csv"($x)`,
		"SELECT * FROM main.query_table($x)",
		"SELECT * FROM glob($x)",
	}
	for _, s := range bad {
		assert.Error(t, checkArtifactSQLShape(s), s)
	}
}

// The built-in skill's examples must be accepted by the validators, so the
// instructions never drift from the format.
func TestDataArtifactsSkill_ExamplesValidate(t *testing.T) {
	var body string
	for _, s := range skills.Builtins() {
		if s.Name == "data-artifacts" {
			body = s.Instructions
		}
	}
	require.NotEmpty(t, body, "the data-artifacts skill is built in")
	re := regexp.MustCompile("(?s)```\\n(.*?)```")
	var manifestSrc, querySrc, viewSrc string
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		switch {
		case strings.Contains(m[1], `"options_query"`):
			manifestSrc = m[1]
		case strings.HasPrefix(strings.TrimSpace(m[1]), `{"name": "customers"`):
			querySrc = m[1]
		case strings.Contains(m[1], `"dateRange"`):
			viewSrc = m[1]
		}
	}
	require.NotEmpty(t, manifestSrc)
	require.NotEmpty(t, querySrc)
	require.NotEmpty(t, viewSrc)

	var manifest map[string]any
	require.NoError(t, json.Unmarshal([]byte(manifestSrc), &manifest))
	var extra map[string]any
	require.NoError(t, json.Unmarshal([]byte(querySrc), &extra))
	manifest["queries"] = append(asSlice(t, manifest["queries"]), extra)
	m, _, err := manifestFromParam(manifest)
	require.NoError(t, err)
	require.NoError(t, artifacts.Validate(m))
	require.NoError(t, artifacts.ValidateView(m, []byte(viewSrc)))
	for _, q := range m.Queries {
		assert.NoError(t, checkArtifactSQLShape(q.SQL), q.Name)
	}
}

// A value is data: a URL or a path as a value is compared, never fetched or
// opened, so neither the policy's EXPLAIN nor the run does any I/O for it.
func TestRunArtifactQuery_AValueNeverCausesIO(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer srv.Close()

	fx := newArtifactFixture(t, ModeFull)
	fx.mustWrite("sales", artifactManifest(rowsQuery(okSQL)))
	for _, v := range []string{srv.URL + "/x?q=secret", fx.p("x.csv"), filepath.Join(fx.outside, "secret.csv"), "s3://lake/raw/x.parquet", "file:///etc/hostname"} {
		n, _, err := fx.run("sales", "rows", map[string]any{"who": v})
		require.NoError(t, err, v)
		assert.Equal(t, 0, n)
	}
	assert.Zero(t, hits.Load(), "no request was made")
}

// A macro that passes its argument into a reader would let a value choose what
// is read; the policy refuses it at save and again at run, whatever the value.
func TestArtifact_AMacroThatReadsItsArgumentIsRefusedAtSaveAndRun(t *testing.T) {
	fx := newArtifactFixture(t, ModeFull)
	fx.exec(`CREATE MACRO read_it(p) AS TABLE SELECT * FROM read_csv(p)`)
	resp := fx.write("leak", artifactManifest(rowsQuery("SELECT * FROM read_it($who)")), artifactView, "")
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrPermission, resp.Error.Code)

	// Hand-edited onto disk, it is refused at run too.
	fx.mustWrite("leak", artifactManifest(rowsQuery(okSQL)))
	path := filepath.Join(fx.root, "artifacts", "leak", "artifact.yaml")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte(strings.Replace(string(data), okSQL, "SELECT * FROM read_it($who)", 1)), 0o644))
	_, _, err = fx.run("leak", "rows", map[string]any{"who": filepath.Join(fx.outside, "secret.csv")})
	require.ErrorIs(t, err, ErrReadOnlyBindingDenied)
}

func TestWriteArtifact_KeepSQLKeepsTheStoredQuery(t *testing.T) {
	long := "SELECT id, name FROM t WHERE id >= $min_id AND id <> 1000000 /* " + strings.Repeat("x", 5400) + " */"
	fx := newArtifactFixture(t, ModeReadWrite)
	rev := fx.mustWrite("sales", artifactManifest(rowsQuery(long)))

	keep := func() map[string]any {
		return artifactManifest(map[string]any{"name": "rows", "keep_sql": true, "sql_omitted": true, "sql_length": float64(5500), "sql": nil, "uses": []any{"min_id"}})
	}
	// Round trip: what get_artifact returns, sent back, keeps the exact SQL.
	got := fx.call("get_artifact", map[string]any{"name": "sales"})
	require.Nil(t, got.Error, errMsg(got))
	back := asRows(t, mapOf(t, mapOf(t, got.Result)["manifest"])["queries"])[0]
	back["keep_sql"] = true
	m := keep()
	m["title"] = "Renamed"
	m["queries"] = []any{back}
	resp := fx.write("sales", m, artifactView, rev)
	require.Nil(t, resp.Error, errMsg(resp))
	stored, err := fx.store.Get(context.Background(), "sales")
	require.NoError(t, err)
	assert.Equal(t, long, stored.Manifest.Queries[0].SQL, "byte for byte")
	assert.Equal(t, "Renamed", stored.Manifest.Title)
	rev = stored.Rev

	// Refused: without a rev, with sql, for an unknown query name.
	resp = fx.write("sales", keep(), artifactView, "")
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "keep_sql needs the rev")
	withSQL := keep()
	withSQL["queries"].([]any)[0].(map[string]any)["sql"] = "SELECT 1" //nolint:errcheck // test literal
	resp = fx.write("sales", withSQL, artifactView, rev)
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "both keep_sql and sql")
	unknown := keep()
	unknown["queries"].([]any)[0].(map[string]any)["name"] = "other" //nolint:errcheck // test literal
	resp = fx.write("sales", unknown, artifactView, rev)
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, `no query named "other"`)
	still, err := fx.store.Get(context.Background(), "sales")
	require.NoError(t, err)
	assert.Equal(t, rev, still.Rev, "refusals change nothing")

	// A person's concurrent edit still fails the write.
	edited := artifacts.Artifact{Name: "sales", Manifest: *mustManifest(t, artifactManifest(rowsQuery("SELECT id, name FROM t"))), View: []byte(artifactView)}
	require.NoError(t, fx.store.Put(context.Background(), &edited, artifacts.WithRev(rev)))
	resp = fx.write("sales", keep(), artifactView, rev)
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "changed since")

	// A full rewrite of a long query works.
	rev = edited.Rev
	resp = fx.write("sales", artifactManifest(rowsQuery(long)), artifactView, rev)
	require.Nil(t, resp.Error, errMsg(resp))
}

func TestWriteArtifact_RevMustBeAString(t *testing.T) {
	fx := newArtifactFixture(t, ModeReadWrite)
	resp := fx.call("write_artifact", map[string]any{"name": "x", "manifest": artifactManifest(rowsQuery(okSQL)), "view": viewMap(t, artifactView), "rev": float64(5)})
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrInvalidParams, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "rev must be a string")
}

func TestArtifactQueriesText_KeptQueryIsLabelled(t *testing.T) {
	manifest := map[string]any{"queries": []any{
		map[string]any{"name": "rows", "keep_sql": true},
		map[string]any{"name": "total", "sql": "SELECT 1"},
	}}
	got := artifactQueriesText(manifest)
	assert.Contains(t, got, "-- rows (kept as stored)")
	assert.Contains(t, got, "-- total\nSELECT 1")
}

// countingEngine counts the statements that reach the engine.
type countingEngine struct {
	EngineAccess
	n atomic.Int32
}

func (c *countingEngine) Execute(ctx context.Context, q string, p map[string]any) (*engine.ResultSet, error) {
	c.n.Add(1)
	return c.EngineAccess.Execute(ctx, q, p)
}

func (c *countingEngine) ExecuteCappedIn(ctx context.Context, conn, q string, p map[string]any, n int) (*engine.ResultSet, int, error) {
	c.n.Add(1)
	inner, ok := c.EngineAccess.(cappedEngine)
	if !ok {
		return nil, 0, fmt.Errorf("engine is not capped")
	}
	return inner.ExecuteCappedIn(ctx, conn, q, p, n)
}

// A result over the byte cap fails while the rows are read, with the fixed
// sentinel, on the push-down path and without a second (unwrapped) attempt. A
// normal query is unaffected.
func TestRunArtifactQuery_ByteCap(t *testing.T) {
	fx := newArtifactFixture(t, ModeReadOnly)
	fxw := newArtifactFixture(t, ModeReadWrite)
	fx.useStore(fxw.store)
	fxw.mustWrite("fat", map[string]any{"title": "Fat", "queries": []any{rowsQuery("SELECT repeat('x', 1000000) AS s FROM range(100)")}})
	fxw.mustWrite("slim", map[string]any{"title": "Slim", "queries": []any{rowsQuery("SELECT repeat('x', 1000) AS s FROM range(100)")}})

	ce := &countingEngine{EngineAccess: fx.h.engine}
	fx.h.engine = ce
	_, _, err := fx.run("fat", "rows", nil)
	require.ErrorIs(t, err, engine.ErrResultTooLarge)
	assert.EqualValues(t, 1, ce.n.Load(), "no second, unwrapped attempt")
	assert.NotContains(t, err.Error(), "repeat")

	n, _, err := fx.run("slim", "rows", nil)
	require.NoError(t, err)
	assert.Equal(t, 100, n)
}
