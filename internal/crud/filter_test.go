package crud

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/session"
)

var filterKnown = map[string]bool{"a": true, "my \"col\"": true, "naïve col": true, "x y": true, "id": true}

func cond(col, op string, v any) FilterCondition {
	return FilterCondition{Column: col, Op: op, Value: v}
}

// The expected strings below were captured from the pre-refactor builders
// (studio buildFilterWhere for the parameterized mode, queries.BuildFilterSQLFrom
// for the inline one), so a change to either shows up here.
func TestBuildWhere_PinnedSQL(t *testing.T) {
	cases := []struct {
		name       string
		spec       FilterSpec
		param      string
		args       []any
		inlineWHER string
	}{
		{"no conditions", FilterSpec{}, "", nil, ""},
		{"eq string with quote", FilterSpec{Conditions: []FilterCondition{cond("a", "eq", "it's")}},
			` WHERE "a" = ?`, []any{"it's"}, ` WHERE "a" = 'it''s'`},
		{"neq int", FilterSpec{Conditions: []FilterCondition{cond("a", "neq", 3)}},
			` WHERE "a" <> ?`, []any{3}, ` WHERE "a" <> 3`},
		{"lt float", FilterSpec{Conditions: []FilterCondition{cond("a", "lt", 1.5)}},
			` WHERE "a" < ?`, []any{1.5}, ` WHERE "a" < 1.5`},
		{"gt int64", FilterSpec{Conditions: []FilterCondition{cond("a", "gt", int64(7))}},
			` WHERE "a" > ?`, []any{int64(7)}, ` WHERE "a" > 7`},
		{"lte true", FilterSpec{Conditions: []FilterCondition{cond("a", "lte", true)}},
			` WHERE "a" <= ?`, []any{true}, ` WHERE "a" <= TRUE`},
		{"gte false", FilterSpec{Conditions: []FilterCondition{cond("a", "gte", false)}},
			` WHERE "a" >= ?`, []any{false}, ` WHERE "a" >= FALSE`},
		{"like wildcards and backslash pass through", FilterSpec{Conditions: []FilterCondition{cond("a", "like", `50%_\x`)}},
			` WHERE "a" LIKE ?`, []any{`50%_\x`}, ` WHERE "a" LIKE '50%_\x'`},
		{"eq nil", FilterSpec{Conditions: []FilterCondition{cond("a", "eq", nil)}},
			` WHERE "a" = ?`, []any{nil}, ` WHERE "a" = NULL`},
		{"null checks bind nothing and ignore the value", FilterSpec{Conditions: []FilterCondition{cond("a", "is_null", "ignored"), cond("id", "is_not_null", nil)}},
			` WHERE "a" IS NULL AND "id" IS NOT NULL`, nil, ` WHERE "a" IS NULL AND "id" IS NOT NULL`},
		{"odd column names, OR, injection-shaped value", FilterSpec{Combinator: " or ", Conditions: []FilterCondition{
			cond("my \"col\"", "eq", "'; DROP TABLE t; --"), cond("naïve col", "like", "é%"), cond("x y", "eq", []int{1})}},
			` WHERE "my ""col""" = ? OR "naïve col" LIKE ? OR "x y" = ?`, []any{"'; DROP TABLE t; --", "é%", []int{1}},
			` WHERE "my ""col""" = '''; DROP TABLE t; --' OR "naïve col" LIKE 'é%' OR "x y" = '[1]'`},
		{"unknown combinator falls back to AND", FilterSpec{Combinator: "xor", Conditions: []FilterCondition{cond("a", "eq", "1"), cond("id", "eq", "2")}},
			` WHERE "a" = ? AND "id" = ?`, []any{"1", "2"}, ` WHERE "a" = '1' AND "id" = '2'`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			where, args, err := BuildWhere(tc.spec, filterKnown)
			require.NoError(t, err)
			assert.Equal(t, tc.param, where)
			assert.Equal(t, tc.args, args)

			inline, err := BuildWhereInline(tc.spec, filterKnown)
			require.NoError(t, err)
			assert.Equal(t, tc.inlineWHER, inline)

			full, err := BuildFilterSQLFrom(`"t"`, tc.spec, nil, filterKnown)
			require.NoError(t, err)
			assert.Equal(t, `SELECT * FROM "t"`+tc.inlineWHER, full)
		})
	}
}

func TestBuildWhere_Rejects(t *testing.T) {
	for name, spec := range map[string]FilterSpec{
		"unknown column":    {Conditions: []FilterCondition{cond("nope", "eq", 1)}},
		"unknown operator":  {Conditions: []FilterCondition{cond("a", "bogus", 1)}},
		"injected column":   {Conditions: []FilterCondition{cond(`a" OR 1=1 --`, "eq", 1)}},
		"injected operator": {Conditions: []FilterCondition{cond("a", "= 1 OR 1=1 --", 1)}},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := BuildWhere(spec, filterKnown)
			assert.Error(t, err)
			_, err = BuildWhereInline(spec, filterKnown)
			assert.Error(t, err)
		})
	}
	_, err := BuildFilterSQLFrom(`"t"`, FilterSpec{}, []string{"nope"}, filterKnown)
	assert.ErrorContains(t, err, `unknown selected column "nope"`)
}

func TestBuildFilterSQL_ProjectionAndTable(t *testing.T) {
	got, err := BuildFilterSQL(`we"ird`, FilterSpec{Conditions: []FilterCondition{cond("a", "eq", 1)}}, []string{"a", "x y"}, filterKnown)
	require.NoError(t, err)
	assert.Equal(t, `SELECT "a", "x y" FROM "we""ird" WHERE "a" = 1`, got)
}

// Inline literals drop NUL bytes (session.QuoteLiteral); main kept them.
func TestBuildWhereInline_NulDropped(t *testing.T) {
	got, err := BuildWhereInline(FilterSpec{Conditions: []FilterCondition{cond("a", "eq", "nul\x00l")}}, filterKnown)
	require.NoError(t, err)
	assert.Equal(t, ` WHERE "a" = 'null'`, got)
}

func TestOrderClause(t *testing.T) {
	known := map[string]bool{"a": true, `we"ird col`: true}
	assert.Equal(t, ` ORDER BY "a" ASC`, OrderClause("a", "", known))
	assert.Equal(t, ` ORDER BY "a" ASC`, OrderClause("a", "sideways", known))
	assert.Equal(t, ` ORDER BY "a" DESC`, OrderClause("a", "desc", known))
	assert.Equal(t, ` ORDER BY "we""ird col" DESC`, OrderClause(`we"ird col`, "DESC", known))
	assert.Equal(t, "", OrderClause("", "DESC", known))
	assert.Equal(t, "", OrderClause(`a"; DROP TABLE t; --`, "DESC", known))
	assert.Equal(t, "", OrderClause("zzz", "DESC", known))
}

func pageTable(t *testing.T) (context.Context, *session.TableRef, func(PageRequest) *PageResult) {
	t.Helper()
	db := hubWithOther(t)
	_, err := db.Exec(`CREATE TABLE people (id INTEGER, "full name" VARCHAR, "we""ird" DOUBLE, born DATE, ok BOOLEAN)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO people VALUES
		(1, 'Ann', 3.5, DATE '2020-01-03', true),
		(2, 'Bob O''Neil', NULL, DATE '2019-05-01', false),
		(3, '100% pure', 1.25, NULL, NULL),
		(4, 'a_b', 9, DATE '2021-12-31', true),
		(5, NULL, -2, DATE '2018-01-01', false)`)
	require.NoError(t, err)
	ref := session.TableRef{Name: "people"}
	ctx := context.Background()
	return ctx, &ref, func(req PageRequest) *PageResult {
		t.Helper()
		res, err := Page(ctx, db, ref, req)
		require.NoError(t, err)
		return res
	}
}

func ids(t *testing.T, res *PageResult) []int {
	t.Helper()
	var out []int
	for _, r := range res.Rows {
		id, ok := r[0].(int32)
		require.True(t, ok)
		out = append(out, int(id))
	}
	return out
}

func TestPage_PagingSortAndTotals(t *testing.T) {
	_, _, page := pageTable(t)

	res := page(PageRequest{PageSize: 2, SortCol: "id"})
	assert.Equal(t, []int{1, 2}, ids(t, res))
	assert.Equal(t, 5, res.TotalRows)
	assert.Equal(t, 3, res.TotalPages)
	assert.Equal(t, []string{"id", "full name", `we"ird`, "born", "ok"}, res.Columns)
	assert.Len(t, res.Types, 5)

	assert.Equal(t, []int{5}, ids(t, page(PageRequest{Page: 3, PageSize: 2, SortCol: "id"})))
	assert.Equal(t, 1, page(PageRequest{Page: -4, PageSize: 2}).Page)
	assert.Equal(t, DefaultPageSize, page(PageRequest{PageSize: 100000}).PageSize)

	// sort on every column type, odd names included; unknown columns are ignored
	assert.Equal(t, []int{5, 4, 3, 2, 1}, ids(t, page(PageRequest{SortCol: "id", SortDir: "desc"})))
	assert.Equal(t, 4, ids(t, page(PageRequest{SortCol: "we\"ird", SortDir: "DESC"}))[0]) // 9.0 is the largest
	assert.Equal(t, 5, ids(t, page(PageRequest{SortCol: "born"}))[0])
	assert.Equal(t, 2, ids(t, page(PageRequest{SortCol: "ok"}))[0])
	assert.Equal(t, 3, ids(t, page(PageRequest{SortCol: "full name"}))[0])
	assert.Len(t, page(PageRequest{SortCol: `id"; DROP TABLE people; --`}).Rows, 5)
}

func TestPage_Filters(t *testing.T) {
	ctx, ref, page := pageTable(t)
	run := func(combinator string, c ...FilterCondition) []int {
		return ids(t, page(PageRequest{SortCol: "id", Filter: FilterSpec{Combinator: combinator, Conditions: c}}))
	}
	assert.Equal(t, []int{2}, run("", cond("full name", "eq", "Bob O'Neil")), "a quote in the value matches literally")
	assert.Equal(t, []int{3}, run("", cond("full name", "like", "100%")), "% is a LIKE wildcard, as before")
	assert.Equal(t, []int{1, 2, 3, 4}, run("", cond("full name", "like", "%_%")))
	assert.Equal(t, []int{5}, run("", cond("full name", "is_null", nil)))
	assert.Equal(t, []int{1, 2, 4, 5}, run("", cond("born", "is_not_null", nil)))
	assert.Equal(t, []int{1, 5}, run("OR", cond("id", "eq", 1), cond("full name", "is_null", nil)))
	assert.Equal(t, []int{1, 4}, run("", cond(`we"ird`, "gt", 3)), "odd column names filter")
	assert.Equal(t, []int{2, 5}, run("", cond("born", "lt", "2019-06-01")))
	assert.Equal(t, []int{1, 4}, run("", cond("ok", "eq", true)))
	assert.Equal(t, []int{2, 3, 4, 5}, run("", cond("id", "neq", 1)))
	assert.Equal(t, []int{1, 2}, run("", cond("id", "lte", 2)))
	assert.Equal(t, []int{4, 5}, run("", cond("id", "gte", 4)))
	assert.Empty(t, run("", cond("full name", "eq", nil)), "= NULL matches nothing; use is_null")

	// the total ignores paging and the empty page still reports one page
	res := page(PageRequest{PageSize: 1, Filter: FilterSpec{Conditions: []FilterCondition{cond("ok", "eq", true)}}})
	assert.Equal(t, 2, res.TotalRows)
	assert.Equal(t, 2, res.TotalPages)
	assert.Len(t, res.Rows, 1)
	none := page(PageRequest{Filter: FilterSpec{Conditions: []FilterCondition{cond("id", "gt", 99)}}})
	assert.Equal(t, 0, none.TotalRows)
	assert.Equal(t, 1, none.TotalPages)

	_, err := Page(ctx, hubWithOther(t), *ref, PageRequest{})
	assert.Error(t, err, "a missing table is an error")
	_, err = Page(ctx, hubWithOther(t), session.TableRef{Name: "items"}, PageRequest{Filter: FilterSpec{Conditions: []FilterCondition{cond("id", "eq", 1)}}})
	assert.ErrorContains(t, err, `unknown filter column "id"`)
}

func TestCreateAndDropTable(t *testing.T) {
	db := hubWithOther(t)
	ctx := context.Background()
	cols := []ColumnDef{{Name: "id", Type: "INTEGER", PK: true}, {Name: "label", Type: "VARCHAR", NotNull: true}}

	require.NoError(t, CreateTable(ctx, db, session.TableRef{Name: "made"}, cols, true))
	_, err := db.Exec(`INSERT INTO made (label) VALUES ('x'), ('y')`)
	require.NoError(t, err)
	var n int
	require.NoError(t, db.QueryRow(`SELECT max(id) FROM made`).Scan(&n))
	assert.Equal(t, 2, n, "the integer PK numbers itself")

	q := session.TableRef{Catalog: "other", Schema: "main", Name: "made2"}
	require.NoError(t, CreateTable(ctx, db, q, cols, false))
	_, err = db.Exec(`INSERT INTO other.main.made2 (label) VALUES ('x')`)
	require.NoError(t, err)

	assert.ErrorContains(t, CreateTable(ctx, db, session.TableRef{Name: "bad"}, []ColumnDef{{Name: `a"b`, Type: "INT"}}, true), "invalid column name")
	require.NoError(t, DropTable(ctx, db, session.TableRef{Name: "made"}))
	require.NoError(t, DropTable(ctx, db, session.TableRef{Name: "made"}), "drop is idempotent")
	assert.Error(t, db.QueryRow(`SELECT 1 FROM made`).Scan(&n))
}

func TestCreateTable_RejectsInjectedColumnType(t *testing.T) {
	db := hubWithOther(t)
	out := filepath.Join(t.TempDir(), "pwned.csv")
	payload := "INTEGER); COPY (SELECT 'pwned' AS x) TO '" + out + "' (FORMAT csv); CREATE TABLE z (a INTEGER"
	err := CreateTable(context.Background(), db, session.TableRef{Name: "victim"}, []ColumnDef{{Name: "id", Type: payload}}, true)
	require.ErrorContains(t, err, "invalid column type")
	assert.NoFileExists(t, out)
	var n int
	assert.Error(t, db.QueryRow(`SELECT count(*) FROM victim`).Scan(&n), "no table was created")

	for _, ok := range []string{"VARCHAR(64)", "DECIMAL(10,2)", "INTEGER PRIMARY KEY", "timestamp", "INTEGER REFERENCES customers(id)", "STRUCT(a INT, b VARCHAR(8))"} {
		assert.True(t, ValidColumnType(ok), ok)
	}
	for _, bad := range []string{"", "  ", "123", "INT;", "INT--", "INT'", `"x"`, "INT/*", "ENUM('a')",
		"INTEGER) AS SELECT a FROM secret_table WHERE (1", "INTEGER)", "VARCHAR(64", ")INTEGER("} {
		assert.False(t, ValidColumnType(bad), bad)
	}
}

func TestPage_SkipCountAndCount(t *testing.T) {
	ctx, ref, page := pageTable(t)
	res := page(PageRequest{SkipCount: true})
	assert.Zero(t, res.TotalRows)
	assert.NotEmpty(t, res.Rows)
	assert.Positive(t, page(PageRequest{}).TotalRows)
	_ = ctx
	_ = ref
}
