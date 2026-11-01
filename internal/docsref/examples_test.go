package docsref

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/assay/internal/notebooks"
	"github.com/SandwichLabs/assay/internal/queries"
	"github.com/SandwichLabs/assay/internal/semantics"
)

// exampleWorkspaces lists docs/examples/*/ (the folders that have an assay.yaml).
func exampleWorkspaces(t *testing.T) []string {
	t.Helper()
	root := filepath.Join(repoRoot(t), "docs", "examples")
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	var dirs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if _, err := os.Stat(filepath.Join(dir, "assay.yaml")); err == nil {
			dirs = append(dirs, dir)
		}
	}
	require.NotEmpty(t, dirs, "docs/examples has no workspace")
	return dirs
}

// TestExampleWorkspacesStillValidate keeps docs/examples/* from rotting. It
// reads the files with the real stores and opens no database and no network:
// the example data is imported by the reader, so nothing here can say what a
// query returns, only that every definition still decodes and fits together.
func TestExampleWorkspacesStillValidate(t *testing.T) {
	for _, dir := range exampleWorkspaces(t) {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			ctx := context.Background()
			for _, f := range []string{"README.md", ".gitignore", ".gitattributes", filepath.Join("semantics", "glossary.md")} {
				_, err := os.Stat(filepath.Join(dir, f))
				assert.NoError(t, err, "%s is missing", f)
			}

			qs := queries.NewFileStore(dir)
			saved, err := qs.List(ctx)
			require.NoError(t, err)
			require.NotEmpty(t, saved, "no saved query")
			ids := map[string]bool{}
			for _, q := range saved {
				assert.Empty(t, q.Error, "saved query %s: %s", q.ID, q.Error)
				ids[q.ID] = true
				// The declared parameters are valid, every $name is declared and
				// every default binds (what `assay query --saved` does first).
				full, err := qs.Get(ctx, q.ID)
				require.NoError(t, err, "saved query %s", q.ID)
				_, err = queries.Bind(full, nil, queries.BindOptions{Strict: true})
				assert.NoError(t, err, "saved query %s", q.ID)
			}

			ss := semantics.NewFileStore(dir)
			entities, err := ss.Entities(ctx)
			require.NoError(t, err)
			metrics, err := ss.Metrics(ctx)
			require.NoError(t, err)
			joins, err := ss.Joins(ctx)
			require.NoError(t, err)
			require.NotEmpty(t, entities)
			require.NotEmpty(t, metrics)

			entityNames := map[string]bool{}
			for _, e := range entities {
				assert.Empty(t, e.Error, "entity %s: %s", e.Name, e.Error)
				entityNames[e.Name] = true
				// An entity's table is a name the import step creates from data/<table>.csv.
				if !strings.Contains(e.Table, ".") {
					_, err := os.Stat(filepath.Join(dir, "data", e.Table+".csv"))
					assert.NoError(t, err, "entity %s: table %s has no data/%s.csv to import", e.Name, e.Table, e.Table)
				}
			}
			for _, m := range metrics {
				assert.Empty(t, m.Error, "metric %s: %s", m.Name, m.Error)
				assert.True(t, ids[m.Query], "metric %s: saved query %q does not exist (queries/%s.sql)", m.Name, m.Query, m.Query)
			}
			for _, j := range joins {
				assert.Empty(t, j.Error, "join %s: %s", j.Name, j.Error)
				assert.True(t, entityNames[j.Left], "join %s: no entity %q", j.Name, j.Left)
				assert.True(t, entityNames[j.Right], "join %s: no entity %q", j.Name, j.Right)
			}

			g, err := ss.Glossary(ctx)
			require.NoError(t, err)
			assert.NotEmpty(t, g.Terms, "glossary has no ## term")

			ns := notebooks.NewFileStore(dir)
			list, err := ns.List(ctx)
			require.NoError(t, err)
			require.NotEmpty(t, list, "no notebook")
			usesMetric := false
			for _, s := range list {
				assert.Empty(t, s.Error, "notebook %s: %s", s.ID, s.Error)
				nb, warnings, err := ns.Get(ctx, s.ID)
				require.NoError(t, err, "notebook %s", s.ID)
				assert.Empty(t, warnings, "notebook %s", s.ID)
				for _, c := range nb.Cells {
					if c.Type != notebooks.CellSQL {
						continue
					}
					usesMetric = usesMetric || strings.Contains(c.Source, "metric(")
					// Every metric() call in a cell expands (names, dimensions and saved queries exist).
					_, err := semantics.Expand(ctx, c.Source, semantics.NewReader(ss, qs), semantics.Options{})
					require.NoError(t, err, "notebook %s, cell %s", s.ID, c.ID)
				}
			}
			assert.True(t, usesMetric, "no notebook cell calls metric()")
		})
	}
}
