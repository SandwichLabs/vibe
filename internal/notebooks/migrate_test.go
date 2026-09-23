package notebooks

import (
	"database/sql"
	"log/slog"
	"strings"
	"testing"

	_ "github.com/mutecomm/go-sqlcipher/v4" // SQLCipher driver registers "sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func migrateTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE _vibe_notebooks (
		id TEXT PRIMARY KEY, title TEXT NOT NULL DEFAULT '', cells TEXT NOT NULL DEFAULT '[]',
		created_at TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL DEFAULT ''
	)`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE _vibe_pages (
		id TEXT PRIMARY KEY, title TEXT NOT NULL DEFAULT '', content TEXT NOT NULL DEFAULT '',
		page_type TEXT NOT NULL DEFAULT 'markdown', sort_order INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL DEFAULT '',
		metadata TEXT NOT NULL DEFAULT '{}', migrated_to TEXT NOT NULL DEFAULT ''
	)`)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

func insertPage(t *testing.T, db *sql.DB, id, title, content, pageType string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO _vibe_pages (id, title, content, page_type, sort_order, created_at, updated_at) VALUES (?, ?, ?, ?, 0, '', '')`,
		id, title, content, pageType,
	)
	require.NoError(t, err)
}

const welcomePage = "---\ntitle: Welcome to Vibe\n---\n\n" +
	"# Welcome to Vibe\n\nThis file contains example data.\n\n" +
	"```sql recent_orders\nSELECT o.id, c.name FROM orders o JOIN customers c ON c.id = o.customer_id LIMIT 5\n```\n\n" +
	"{table: recent_orders, sortable: true}\n\n## What's Next?\n\nMore stuff here.\n"

func TestMigratePages_WelcomeFixture(t *testing.T) {
	db := migrateTestDB(t)
	insertPage(t, db, "welcome", "Welcome to Vibe", welcomePage, "markdown")

	n, err := MigratePages(db, slog.Default())
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	var migratedTo string
	require.NoError(t, db.QueryRow(`SELECT migrated_to FROM _vibe_pages WHERE id = 'welcome'`).Scan(&migratedTo))
	require.NotEmpty(t, migratedTo)

	nb, _, err := Get(db, migratedTo)
	require.NoError(t, err)
	assert.Equal(t, "Welcome to Vibe", nb.Title)

	var sqlCell *Cell
	for i := range nb.Cells {
		if nb.Cells[i].Type == CellSQL {
			sqlCell = &nb.Cells[i]
		}
	}
	require.NotNil(t, sqlCell, "expected a migrated sql cell from the {table:} directive")
	assert.Equal(t, "table", sqlCell.View)
	assert.True(t, sqlCell.ViewPinned)
	assert.Equal(t, "recent_orders", sqlCell.Label)
	assert.Contains(t, sqlCell.Source, "SELECT o.id")

	var hasMarkdown bool
	for _, c := range nb.Cells {
		if c.Type == CellMarkdown {
			hasMarkdown = true
		}
	}
	assert.True(t, hasMarkdown, "prose should become a markdown cell")
}

const dashboardPage = "---\ntitle: Dashboard\n---\n\n# Sales Dashboard\n\n" +
	"```sql revenue_by_region\nSELECT region, SUM(total) FROM orders GROUP BY region\n```\n\n" +
	"{chart: revenue_by_region, type: bar, x: region, y: revenue, title: Revenue by Region}\n\n" +
	"```sql one_metric\nSELECT COUNT(*) FROM orders\n```\n\n" +
	"{metric: one_metric, label: Orders}\n"

func TestMigratePages_ChartAndMetricDirectives(t *testing.T) {
	db := migrateTestDB(t)
	insertPage(t, db, "dashboard", "Dashboard", dashboardPage, "markdown")

	_, err := MigratePages(db, nil)
	require.NoError(t, err)

	var migratedTo string
	require.NoError(t, db.QueryRow(`SELECT migrated_to FROM _vibe_pages WHERE id = 'dashboard'`).Scan(&migratedTo))
	nb, _, err := Get(db, migratedTo)
	require.NoError(t, err)

	views := map[string]bool{}
	for _, c := range nb.Cells {
		if c.Type == CellSQL {
			views[c.View] = true
		}
	}
	assert.True(t, views["bar"], "chart:type=bar should become a bar cell")
	assert.True(t, views["stat"], "metric: should become a stat cell")
}

const formPage = "# Form Page\n\nSome text before.\n\n" +
	"{form: table=customers, title=Edit Customer}\n\nSome text after.\n"

func TestMigratePages_FormDirectiveNotMigrated(t *testing.T) {
	db := migrateTestDB(t)
	insertPage(t, db, "formpage", "Form Page", formPage, "markdown")

	_, err := MigratePages(db, nil)
	require.NoError(t, err)

	var migratedTo string
	require.NoError(t, db.QueryRow(`SELECT migrated_to FROM _vibe_pages WHERE id = 'formpage'`).Scan(&migratedTo))
	nb, _, err := Get(db, migratedTo)
	require.NoError(t, err)

	var noteFound bool
	for _, c := range nb.Cells {
		if c.Type == CellMarkdown && containsAll(c.Source, "Not migrated", "form") {
			noteFound = true
		}
	}
	assert.True(t, noteFound, "expected a 'Not migrated: form' note, cells: %+v", nb.Cells)
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !contains(s, sub) {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

func TestMigratePages_HTMLPage(t *testing.T) {
	db := migrateTestDB(t)
	insertPage(t, db, "htmlpage", "HTML Page", "<div>raw html</div>", "html")

	_, err := MigratePages(db, nil)
	require.NoError(t, err)

	var migratedTo string
	require.NoError(t, db.QueryRow(`SELECT migrated_to FROM _vibe_pages WHERE id = 'htmlpage'`).Scan(&migratedTo))
	nb, _, err := Get(db, migratedTo)
	require.NoError(t, err)
	require.Len(t, nb.Cells, 1)
	assert.Equal(t, CellMarkdown, nb.Cells[0].Type)
	assert.Contains(t, nb.Cells[0].Source, "Not migrated: raw HTML")
	assert.Contains(t, nb.Cells[0].Source, "raw html")
}

func TestMigratePages_Idempotent(t *testing.T) {
	db := migrateTestDB(t)
	insertPage(t, db, "welcome", "Welcome to Vibe", welcomePage, "markdown")

	n1, err := MigratePages(db, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, n1)

	n2, err := MigratePages(db, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, n2, "a second run must not re-migrate already-migrated pages")

	list, err := List(db)
	require.NoError(t, err)
	assert.Len(t, list, 1, "must not create a duplicate notebook")
}

func TestMigratePages_SkipsSystemPages(t *testing.T) {
	db := migrateTestDB(t)
	insertPage(t, db, "_dashboard", "Dashboard", "# hi", "markdown")

	n, err := MigratePages(db, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, n)

	list, err := List(db)
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestMigratePages_VarParamNote(t *testing.T) {
	db := migrateTestDB(t)
	content := "# P\n\n```sql filtered\nSELECT * FROM orders WHERE region = ${region}\n```\n\n{table: filtered}\n"
	insertPage(t, db, "p", "P", content, "markdown")

	_, err := MigratePages(db, nil)
	require.NoError(t, err)

	var migratedTo string
	require.NoError(t, db.QueryRow(`SELECT migrated_to FROM _vibe_pages WHERE id = 'p'`).Scan(&migratedTo))
	nb, _, err := Get(db, migratedTo)
	require.NoError(t, err)

	var noteFound bool
	for _, c := range nb.Cells {
		if c.Type == CellMarkdown && contains(c.Source, "${var}") {
			noteFound = true
		}
	}
	assert.True(t, noteFound)
}

const richMarkdownPage = "# Rich Formatting\n\n" +
	"## Quarterly review\n\n" +
	"This notebook checks **revenue** by region, see [the dashboard](https://example.com/dash).\n\n" +
	"- bullet one\n" +
	"- bullet two\n\n" +
	"1. step one\n" +
	"2. step two\n\n" +
	"> a blockquote worth keeping\n\n" +
	"```sql rev\nSELECT 1\n```\n\n" +
	"{table: rev}\n\n" +
	"Trailing *emphasis* paragraph.\n"

// TestMigratePages_PreservesMarkdownFormatting (review fix, step 4a #5):
// goldmark's inline parser strips **/_/>/-/1./link-URL markers from a
// TextNode's reconstructed .Content (they become structural Emphasis/Link
// nodes wrapping a bare Text node) — migration must build markdown cells
// from the ORIGINAL raw lines, not from that stripped text, or every
// migrated page loses its formatting.
func TestMigratePages_PreservesMarkdownFormatting(t *testing.T) {
	db := migrateTestDB(t)
	insertPage(t, db, "rich", "Rich", richMarkdownPage, "markdown")

	_, err := MigratePages(db, nil)
	require.NoError(t, err)

	var migratedTo string
	require.NoError(t, db.QueryRow(`SELECT migrated_to FROM _vibe_pages WHERE id = 'rich'`).Scan(&migratedTo))
	nb, _, err := Get(db, migratedTo)
	require.NoError(t, err)

	var allMarkdown string
	for _, c := range nb.Cells {
		if c.Type == CellMarkdown {
			allMarkdown += c.Source + "\n"
		}
	}
	for _, marker := range []string{"## Quarterly review", "**revenue**", "- bullet one", "1. step one", "> a blockquote", "(https://example.com/dash)", "*emphasis*"} {
		assert.Contains(t, allMarkdown, marker, "formatting marker %q should survive migration", marker)
	}
}

// TestMigratePages_RowOfMetricsSurvives (review fix, step 4a round 2 #3):
// a {row:} grouping two {metric:} directives must still produce two stat
// cells — only the grid grouping itself is dropped, with a short note.
func TestMigratePages_RowOfMetricsSurvives(t *testing.T) {
	content := "# Dashboard\n\n" +
		"```sql tot\nSELECT SUM(revenue) AS revenue FROM sales\n```\n\n" +
		"{row:}\n\n" +
		"{metric: tot, label: \"Total\"}\n\n" +
		"{metric: tot, label: \"Again\"}\n\n" +
		"{/row}\n\n" +
		"Closing text.\n"
	db := migrateTestDB(t)
	insertPage(t, db, "p", "P", content, "markdown")

	_, err := MigratePages(db, nil)
	require.NoError(t, err)

	var migratedTo string
	require.NoError(t, db.QueryRow(`SELECT migrated_to FROM _vibe_pages WHERE id = 'p'`).Scan(&migratedTo))
	nb, _, err := Get(db, migratedTo)
	require.NoError(t, err)

	var statCells int
	var rowNoteFound, closingFound bool
	for _, c := range nb.Cells {
		if c.Type == CellSQL && c.View == "stat" {
			statCells++
		}
		if c.Type == CellMarkdown && contains(c.Source, "row") && contains(c.Source, "Not migrated") {
			rowNoteFound = true
		}
		if c.Type == CellMarkdown && contains(c.Source, "Closing text") {
			closingFound = true
		}
	}
	assert.Equal(t, 2, statCells, "both {metric:} directives inside {row:} must survive as stat cells, cells: %+v", nb.Cells)
	assert.True(t, rowNoteFound, "expected a short 'Not migrated: row' note")
	assert.True(t, closingFound, "prose after the {/row} close must still be recovered")
}

// TestMigratePages_IfWithTableSurvives (review fix, step 4a round 2 #3): a
// {if:} wrapping a {table:} directive must still produce a real sql cell —
// only the conditional itself is dropped.
func TestMigratePages_IfWithTableSurvives(t *testing.T) {
	content := "# P\n\n" +
		"```sql rows\nSELECT 1 AS x\n```\n\n" +
		"{if: rows}\n\n" +
		"{table: rows}\n\n" +
		"{/if}\n"
	db := migrateTestDB(t)
	insertPage(t, db, "p", "P", content, "markdown")

	_, err := MigratePages(db, nil)
	require.NoError(t, err)

	var migratedTo string
	require.NoError(t, db.QueryRow(`SELECT migrated_to FROM _vibe_pages WHERE id = 'p'`).Scan(&migratedTo))
	nb, _, err := Get(db, migratedTo)
	require.NoError(t, err)

	var tableCellFound bool
	for _, c := range nb.Cells {
		if c.Type == CellSQL && c.View == "table" && c.ViewPinned {
			tableCellFound = true
		}
	}
	assert.True(t, tableCellFound, "the {table:} directive inside {if:} must survive as a pinned table cell, cells: %+v", nb.Cells)
}

// TestMigratePages_NoFenceOpenerLeaksIntoProse (review fix, step 4a round 2
// #4): SQLBlockNode.Line is the first SQL BODY line, not the ```sql opener
// line — flushing the prose gap up to node.Line-1 used to include the
// opener itself in the preceding markdown cell. Covers the plain, CRLF, and
// unterminated-fence cases.
func TestMigratePages_NoFenceOpenerLeaksIntoProse(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{
			name: "plain",
			content: "# Title\n\nIntro text.\n\n" +
				"```sql by_region\nSELECT region FROM sales\n```\n\n" +
				"{table: by_region}\n\nClosing text.\n",
		},
		{
			name: "crlf",
			content: strings.ReplaceAll(
				"# Title\n\nIntro text.\n\n"+
					"```sql by_region\nSELECT region FROM sales\n```\n\n"+
					"{table: by_region}\n\nClosing text.\n",
				"\n", "\r\n"),
		},
		{
			name:    "unterminated",
			content: "# Unterminated\n\nSome text.\n\n```sql broken\nSELECT 1 AS x\n\nMore prose after.\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := migrateTestDB(t)
			insertPage(t, db, "p", "P", tc.content, "markdown")

			_, err := MigratePages(db, nil)
			require.NoError(t, err)

			var migratedTo string
			require.NoError(t, db.QueryRow(`SELECT migrated_to FROM _vibe_pages WHERE id = 'p'`).Scan(&migratedTo))
			nb, _, err := Get(db, migratedTo)
			require.NoError(t, err)

			for _, c := range nb.Cells {
				if c.Type != CellMarkdown {
					continue
				}
				assert.NotContains(t, c.Source, "```sql", "a ```sql fence opener must never leak into a markdown cell (cell: %q)", c.Source)
				assert.False(t, strings.Contains(c.Source, "```") && strings.Contains(c.Source, "SELECT"),
					"SQL body content must never appear inside a markdown cell alongside a fence marker (cell: %q)", c.Source)
			}
		})
	}
}

// TestMigrateOnePage_AtomicOnFailure (review fix): if the migrated_to stamp
// fails after the notebook was already inserted, the insert must be rolled
// back too — otherwise a re-run would insert a SECOND notebook for the same
// page (migrated_to still empty, so MigratePages would pick it up again),
// leaving an orphaned first notebook nothing points to. Simulated by a
// trigger that aborts the UPDATE ... SET migrated_to statement specifically,
// leaving the initial page SELECT and the notebook INSERT unaffected.
func TestMigrateOnePage_AtomicOnFailure(t *testing.T) {
	db := migrateTestDB(t)
	insertPage(t, db, "welcome", "Welcome to Vibe", welcomePage, "markdown")

	_, err := db.Exec(`CREATE TRIGGER fail_stamp BEFORE UPDATE OF migrated_to ON _vibe_pages
		BEGIN SELECT RAISE(ABORT, 'simulated failure between insert and stamp'); END;`)
	require.NoError(t, err)

	n, err := MigratePages(db, nil)
	require.NoError(t, err) // MigratePages logs and skips a single failed page rather than failing the whole run
	assert.Equal(t, 0, n)

	list, err := List(db)
	require.NoError(t, err)
	assert.Empty(t, list, "a failed migrated_to stamp must roll back the notebook insert in the same transaction")

	var migratedTo string
	require.NoError(t, db.QueryRow(`SELECT migrated_to FROM _vibe_pages WHERE id = 'welcome'`).Scan(&migratedTo))
	assert.Empty(t, migratedTo)
}

func TestMigratePages_NeverDeletesPageRows(t *testing.T) {
	db := migrateTestDB(t)
	insertPage(t, db, "welcome", "Welcome to Vibe", welcomePage, "markdown")

	_, err := MigratePages(db, nil)
	require.NoError(t, err)

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM _vibe_pages`).Scan(&count))
	assert.Equal(t, 1, count)
}
