package lsp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// fixture is a workspace with a local DuckDB default and a Postgres connection.
func fixture() *Catalog {
	return &Catalog{
		Version:           catalogVersion,
		DefaultConnection: "local",
		Functions:         []string{"count", "date_trunc", "sum"},
		Connections: []Connection{
			{Name: "local", Type: "duckdb", DefaultSchema: "main", Schemas: []Schema{
				{Name: "main", Tables: []Relation{
					{Name: "orders", Kind: "table", Columns: []Column{{"id", "INTEGER"}, {"customer_id", "INTEGER"}, {"total", "DECIMAL(10,2)"}}},
					{Name: "customers", Kind: "table", Columns: []Column{{"id", "INTEGER"}, {"name", "VARCHAR"}}},
					{Name: "Order Lines", Kind: "view", Columns: []Column{{"qty", "INTEGER"}}},
				}},
				{Name: "staging", Tables: []Relation{
					{Name: "raw_events", Kind: "table", Columns: []Column{{"payload", "JSON"}}},
				}},
			}},
			{Name: "pg", Type: "postgres", DefaultSchema: "public", Schemas: []Schema{
				{Name: "public", Tables: []Relation{
					{Name: "invoices", Kind: "table", Columns: []Column{{"invoice_no", "VARCHAR"}, {"amount", "NUMERIC"}}},
				}},
				{Name: "billing", Tables: []Relation{
					{Name: "plans", Kind: "table", Columns: []Column{{"plan_id", "INTEGER"}}},
				}},
			}},
		},
	}
}

// at completes src where "|" marks the cursor.
func at(src string, cat *Catalog) []Item {
	off := strings.Index(src, "|")
	return Complete(strings.Replace(src, "|", "", 1), off, cat)
}

func labels(items []Item) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Label)
	}
	return out
}

func labelsOf(items []Item, kind ItemKind) []string {
	var out []string
	for _, it := range items {
		if it.Kind == kind {
			out = append(out, it.Label)
		}
	}
	return out
}

func TestComplete_RelationsAfterFrom(t *testing.T) {
	items := at("SELECT * FROM |", fixture())
	assert.ElementsMatch(t, []string{"orders", "customers"}, labelsOf(items, KindTable))
	assert.Equal(t, []string{"Order Lines"}, labelsOf(items, KindView))
	assert.ElementsMatch(t, []string{"local", "pg"}, labelsOf(items, KindConnection))
	assert.Contains(t, labelsOf(items, KindSchema), "staging")

	items = at("SELECT * FROM ord|", fixture())
	assert.Equal(t, []string{"orders"}, labelsOf(items, KindTable), "filtered by the typed prefix, any case")
	assert.Empty(t, labelsOf(items, KindColumn))
}

func TestComplete_AfterJoinAndCommaList(t *testing.T) {
	assert.Contains(t, labels(at("SELECT * FROM orders o JOIN cu|", fixture())), "customers")
	assert.Contains(t, labels(at("SELECT * FROM orders o, cu|", fixture())), "customers")
}

func TestComplete_QuotesNamesThatNeedIt(t *testing.T) {
	for _, it := range at("FROM Ord|", fixture()) {
		if it.Label == "Order Lines" {
			assert.Equal(t, `"Order Lines"`, it.Insert)
			return
		}
	}
	t.Fatal("view not offered")
}

func TestComplete_ConnectionAndSchemaPaths(t *testing.T) {
	items := at("SELECT * FROM pg.|", fixture())
	assert.ElementsMatch(t, []string{"public", "billing"}, labelsOf(items, KindSchema))
	assert.Equal(t, []string{"invoices"}, labelsOf(items, KindTable), "catalog.table binds to its default schema")

	assert.Equal(t, []string{"plans"}, labelsOf(at("SELECT * FROM pg.billing.|", fixture()), KindTable))
	assert.Equal(t, []string{"raw_events"}, labelsOf(at("SELECT * FROM staging.|", fixture()), KindTable))
}

func TestComplete_ColumnsThroughAliases(t *testing.T) {
	items := at("SELECT o.| FROM orders o JOIN customers c ON o.customer_id = c.id", fixture())
	assert.Equal(t, []string{"id", "customer_id", "total"}, labelsOf(items, KindColumn))

	items = at("SELECT c.na| FROM orders o JOIN customers AS c ON o.customer_id = c.id", fixture())
	assert.Equal(t, []string{"name"}, labels(items))

	items = at("SELECT i.| FROM pg.public.invoices i", fixture())
	assert.Equal(t, []string{"invoice_no", "amount"}, labelsOf(items, KindColumn))

	items = at("SELECT orders.| FROM orders", fixture())
	assert.Equal(t, []string{"id", "customer_id", "total"}, labelsOf(items, KindColumn), "a table name works as its own alias")
}

func TestComplete_BareColumnsFromTheStatementsTables(t *testing.T) {
	items := at("SELECT | FROM orders o JOIN customers c ON true", fixture())
	cols := labelsOf(items, KindColumn)
	assert.ElementsMatch(t, []string{"id", "customer_id", "total", "name"}, cols, "deduplicated across tables")
	assert.ElementsMatch(t, []string{"o", "c"}, labelsOf(items, KindAlias))
	assert.Contains(t, labelsOf(items, KindFunction), "date_trunc")
	assert.Contains(t, labelsOf(items, KindKeyword), "WHERE")
	assert.Equal(t, 0, items[0].Kind.Rank(), "the statement's columns and aliases sort first")
	assert.Equal(t, KindKeyword, items[len(items)-1].Kind, "keywords sort last")

	items = at("SELECT * FROM orders WHERE tot|", fixture())
	assert.Equal(t, []string{"total"}, labels(items))
}

func TestComplete_OnlyTheCurrentStatement(t *testing.T) {
	items := at("SELECT * FROM customers; SELECT | FROM orders", fixture())
	assert.NotContains(t, labelsOf(items, KindColumn), "name")
	assert.Contains(t, labelsOf(items, KindColumn), "total")
}

func TestComplete_NothingInStringsOrComments(t *testing.T) {
	assert.Empty(t, at("SELECT * FROM orders WHERE x = 'or|", fixture()))
	assert.Empty(t, at("SELECT 1 -- from or|", fixture()))
	assert.Empty(t, at("SELECT /* or| */ 1", fixture()))
	assert.NotEmpty(t, at("SELECT 'x', | FROM orders", fixture()))
}

func TestComplete_StartOfStatementOffersKeywords(t *testing.T) {
	items := at("SEL|", fixture())
	assert.Equal(t, []string{"SELECT"}, labels(items))
}

func TestComplete_NoSnapshotStillOffersKeywords(t *testing.T) {
	items := at("SELECT * FROM orders WHER|", nil)
	assert.Equal(t, []string{"WHERE"}, labels(items))
}

func TestHover(t *testing.T) {
	hov := func(src string) string {
		off := strings.Index(src, "|")
		return Hover(strings.Replace(src, "|", "", 1), off, fixture())
	}
	assert.Contains(t, hov("SELECT * FROM ord|ers"), "**table** `local.main.orders`")
	assert.Contains(t, hov("SELECT * FROM ord|ers"), "`total` DECIMAL(10,2)")
	assert.Contains(t, hov("SELECT * FROM pg.billing.pl|ans"), "`pg.billing.plans`")
	assert.Contains(t, hov("SELECT o.tot|al FROM orders o"), "**column** `total` DECIMAL(10,2)")
	assert.Contains(t, hov("SELECT na|me FROM customers"), "`name` VARCHAR")
	assert.Contains(t, hov("SELECT * FROM p|g.public.invoices"), "postgres connection")
	assert.Empty(t, hov("SELECT nope| FROM orders"))
}
