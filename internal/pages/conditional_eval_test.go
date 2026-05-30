package pages_test

// TDD RED — Phase 3 / unit P3.1: "Page-level conditional ({if}) evaluation + render"
// (parity plan §3, Evidence WU-5 close-out).
//
// This is a REAL integration test: it opens a real encrypted .vibe container,
// materializes DuckDB, seeds a table, and executes a parsed page that contains
// {if}/{/if} conditional blocks through the production pages.Execute path with
// a real engine.Manager-backed QueryEngine. No mocks.
//
// Acceptance behavior asserted here:
//   - {if: "orders"} renders its children only when the named query returned >0 rows
//   - {if: "orders", condition: "no_rows"} renders its children only when 0 rows
//   - The surviving branch's children are emitted INTO ExecutedPage.Nodes; the
//     dropped branch contributes nothing (no node, no error)
//   - Re-executing after truncating the table flips which branch survives

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/sandwich-labs/vibe-app/internal/engine"
	"github.com/sandwich-labs/vibe-app/internal/pages"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// realEngine adapts a real engine.Manager (DuckDB) to the pages.QueryEngine
// interface, translating the engine ResultSet type into the pages one.
type realEngine struct {
	mgr *engine.Manager
}

func (r *realEngine) Execute(ctx context.Context, sql string, params map[string]any) (*pages.ResultSet, error) {
	rs, err := r.mgr.Execute(ctx, sql, params)
	if err != nil {
		return nil, err
	}
	cols := make([]pages.Column, len(rs.Columns))
	for i, c := range rs.Columns {
		cols[i] = pages.Column{Name: c.Name, Type: c.Type}
	}
	return &pages.ResultSet{Columns: cols, Rows: rs.Rows}, nil
}

// containsTextNode reports whether the executed node list contains a top-level
// TextNode whose content includes the given substring. Conditional children
// must be inlined into the node list for a matching branch.
func containsTextNode(nodes []pages.ExecutedNode, substr string) bool {
	for _, en := range nodes {
		if tn, ok := en.Node.(*pages.TextNode); ok {
			if contains(tn.Content, substr) {
				return true
			}
		}
	}
	return false
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func newRealEngine(t *testing.T) (*realEngine, *container.Container) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "cond.vibe")
	c, err := container.Create(path, container.WithPassphrase("test-passphrase-12345"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	ddb := c.DuckDBDB()
	require.NotNil(t, ddb)
	_, err = ddb.Exec(`CREATE TABLE orders (id INTEGER, total DECIMAL(10,2));`)
	require.NoError(t, err)
	_, err = ddb.Exec(`INSERT INTO orders (id, total) VALUES (1, 10.00), (2, 20.00);`)
	require.NoError(t, err)

	return &realEngine{mgr: engine.NewManager(ddb)}, c
}

const condPageSrc = "" +
	"```sql orders\n" +
	"SELECT id, total FROM orders\n" +
	"```\n\n" +
	"{if: \"orders\"}\n\n" +
	"ORDERS_PRESENT_BRANCH\n\n" +
	"{/if}\n\n" +
	"{if: \"orders\", condition: \"no_rows\"}\n\n" +
	"NO_ORDERS_BRANCH\n\n" +
	"{/if}\n"

func TestConditionalEvaluatesRowsPresentBranch(t *testing.T) {
	eng, c := newRealEngine(t)

	page, err := pages.Parse(condPageSrc)
	require.NoError(t, err)

	executed, err := pages.Execute(context.Background(), page, eng, nil)
	require.NoError(t, err)

	// With rows present, only the rows-present branch's children survive.
	assert.True(t, containsTextNode(executed.Nodes, "ORDERS_PRESENT_BRANCH"),
		"rows-present branch children must be inlined into ExecutedPage.Nodes when the query returns rows")
	assert.False(t, containsTextNode(executed.Nodes, "NO_ORDERS_BRANCH"),
		"the no_rows branch must NOT survive when the query returns rows")

	// No stray ConditionalNode should remain in the executed output — branches
	// are resolved to their surviving children.
	for _, en := range executed.Nodes {
		_, isCond := en.Node.(*pages.ConditionalNode)
		assert.False(t, isCond, "conditional nodes must be evaluated away, not passed through")
	}

	// Now truncate the table and re-execute: the no_rows branch should win.
	_, err = c.DuckDBDB().Exec(`DELETE FROM orders;`)
	require.NoError(t, err)

	executed2, err := pages.Execute(context.Background(), page, eng, nil)
	require.NoError(t, err)

	assert.True(t, containsTextNode(executed2.Nodes, "NO_ORDERS_BRANCH"),
		"no_rows branch children must survive when the query returns zero rows")
	assert.False(t, containsTextNode(executed2.Nodes, "ORDERS_PRESENT_BRANCH"),
		"the rows-present branch must NOT survive when the query returns zero rows")
}

// varEqPageSrc exercises the variable-equality condition form using both `==`
// and `!=` against a {{status}} variable.
const varEqPageSrc = "" +
	"{if: \"{{status}} == 'active'\"}\n\n" +
	"ACTIVE_EQ_BRANCH\n\n" +
	"{/if}\n\n" +
	"{if: \"{{status}} != 'active'\"}\n\n" +
	"NOT_ACTIVE_BRANCH\n\n" +
	"{/if}\n"

func TestConditionalEvaluatesVariableEquality(t *testing.T) {
	eng, _ := newRealEngine(t)

	page, err := pages.Parse(varEqPageSrc)
	require.NoError(t, err)

	// When status == "active": the == branch survives, the != branch is dropped.
	executed, err := pages.Execute(context.Background(), page, eng, map[string]any{"status": "active"})
	require.NoError(t, err)
	assert.True(t, containsTextNode(executed.Nodes, "ACTIVE_EQ_BRANCH"),
		"== branch must survive when the variable equals the literal")
	assert.False(t, containsTextNode(executed.Nodes, "NOT_ACTIVE_BRANCH"),
		"!= branch must be dropped when the variable equals the literal")
	for _, en := range executed.Nodes {
		_, isCond := en.Node.(*pages.ConditionalNode)
		assert.False(t, isCond, "conditional nodes must be evaluated away")
	}

	// When status != "active": the == branch is dropped, the != branch survives.
	executed2, err := pages.Execute(context.Background(), page, eng, map[string]any{"status": "archived"})
	require.NoError(t, err)
	assert.False(t, containsTextNode(executed2.Nodes, "ACTIVE_EQ_BRANCH"),
		"== branch must be dropped when the variable differs from the literal")
	assert.True(t, containsTextNode(executed2.Nodes, "NOT_ACTIVE_BRANCH"),
		"!= branch must survive when the variable differs from the literal")

	// When status is unset, it resolves to empty string: both differ from
	// "active", so only the != branch survives.
	executed3, err := pages.Execute(context.Background(), page, eng, nil)
	require.NoError(t, err)
	assert.False(t, containsTextNode(executed3.Nodes, "ACTIVE_EQ_BRANCH"),
		"== branch must be dropped when the variable is unset")
	assert.True(t, containsTextNode(executed3.Nodes, "NOT_ACTIVE_BRANCH"),
		"!= branch must survive when the variable is unset (empty != 'active')")
}

// TestConditionalRejectsUnknownMode verifies the parser rejects an unknown
// condition mode rather than silently accepting it.
func TestConditionalRejectsUnknownMode(t *testing.T) {
	src := "" +
		"{if: \"orders\", condition: \"sometimes\"}\n\n" +
		"BRANCH\n\n" +
		"{/if}\n"
	_, err := pages.Parse(src)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown conditional mode")
}
