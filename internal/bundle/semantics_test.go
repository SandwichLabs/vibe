package bundle

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/slaw/internal/queries"
	"github.com/SandwichLabs/slaw/internal/semantics"
	"github.com/SandwichLabs/slaw/internal/workspace"
)

// Hand-written on purpose: comments and flow style that a re-encode would lose.
const semEntity = `# who buys
version: 1
name: customer
grain: one row per customer
table: prod_pg.public.customers
id_column: customer_id
columns:
  - {name: email, pii: true}
`

const semMetric = `# kept as written, comment and all
version: 1
name: net_revenue
title: Net revenue
query: net_revenue_base
measure: sum(amount)
dimensions: [region]
`

const semJoin = `version: 1
name: orders_customers
left: orders
right: customer
on: [{left: customer_id, right: customer_id}]
cardinality: many_to_one
`

const semGlossary = "# Glossary\n\n## Active customer\n\nBought in the last 90 days.\n"

var semFiles = map[string]string{
	"semantics/entities/customer.yaml":      semEntity,
	"semantics/metrics/net_revenue.yaml":    semMetric,
	"semantics/joins/orders_customers.yaml": semJoin,
	"semantics/glossary.md":                 semGlossary,
}

func writeSem(t *testing.T, ws workspace.Workspace, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(ws.Root(), filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	}
}

func readSem(t *testing.T, ws workspace.Workspace, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(ws.Root(), filepath.FromSlash(rel)))
	require.NoError(t, err, rel)
	return string(b)
}

func TestSemantics_RoundTripKeepsBytes(t *testing.T) {
	src, _ := newWS(t)
	writeSem(t, src, semFiles)

	ctx := context.Background()
	res, err := Export(ctx, src, ExportOptions{Selection: all(), AppVersion: "test"})
	require.NoError(t, err)
	require.Len(t, res.Manifest.Contents, 4)
	paths := map[string]ItemKind{}
	for _, e := range res.Manifest.Contents {
		paths[e.Path] = e.Kind
	}
	for rel := range semFiles {
		assert.Equal(t, KindSemantics, paths[rel], rel)
	}
	assert.Equal(t, 4, res.Manifest.Counts().Semantics)

	dst, _ := newWS(t)
	pkg, err := Open(res.Data, "")
	require.NoError(t, err)
	pv := pkg.Preview(ctx, dst)
	require.Len(t, pv.Items, 4)
	titles := map[string]string{}
	for _, it := range pv.Items {
		assert.False(t, it.Conflict)
		titles[it.Key] = it.Title
	}
	assert.Equal(t, "Net revenue", titles["semantics/metrics/net_revenue"])
	assert.Equal(t, "Glossary", titles["semantics/glossary"])

	out, err := pkg.Import(ctx, dst, ImportOptions{})
	require.NoError(t, err)
	require.Zero(t, out.Failed(), "%+v", out.Outcomes)
	for rel, want := range semFiles {
		assert.Equal(t, want, readSem(t, dst, rel), rel)
	}
	got, err := dst.Semantics().Metric(ctx, "net_revenue")
	require.NoError(t, err)
	assert.Equal(t, "sum(amount)", got.Measure)
}

func TestSemantics_SelectAndContentsOf(t *testing.T) {
	src, _ := newWS(t)
	writeSem(t, src, semFiles)
	writeSem(t, src, map[string]string{"semantics/metrics/broken.yaml": "version: 1\nname: broken\n"})
	ctx := context.Background()

	c, err := ContentsOf(ctx, src)
	require.NoError(t, err)
	ids := map[string]string{}
	for _, it := range c.Semantics {
		ids[it.ID] = it.Title
	}
	assert.Equal(t, map[string]string{
		"entities/customer":      "customer",
		"metrics/net_revenue":    "Net revenue",
		"joins/orders_customers": "orders_customers",
		"glossary":               "Glossary",
	}, ids, "an unreadable file is not offered")

	res, err := Export(ctx, src, ExportOptions{Selection: Selection{Semantics: []string{"metrics/net_revenue"}}})
	require.NoError(t, err)
	require.Len(t, res.Manifest.Contents, 1)
	assert.Equal(t, "semantics/metrics/net_revenue.yaml", res.Manifest.Contents[0].Path)

	// Everything, with the broken file skipped and said so.
	res, err = Export(ctx, src, ExportOptions{Selection: all()})
	require.NoError(t, err)
	assert.Len(t, res.Manifest.Contents, 4)
	assert.NotEmpty(t, res.Manifest.Warnings)
	assert.Contains(t, strings.Join(res.Manifest.Warnings, "\n"), "semantics/metrics/broken.yaml was skipped")

	// Asking for it by name fails the export.
	_, err = Export(ctx, src, ExportOptions{Selection: Selection{Semantics: []string{"metrics/broken"}}})
	require.Error(t, err)
	_, err = Export(ctx, src, ExportOptions{Selection: Selection{Semantics: []string{"metrics/Bad Name"}}})
	require.Error(t, err)
}

func TestSemantics_OpenRefusesWholeBundle(t *testing.T) {
	good := func() map[string][]byte {
		return map[string][]byte{
			"semantics/metrics/net_revenue.yaml": []byte(semMetric),
			"semantics/glossary.md":              []byte(semGlossary),
		}
	}
	set := func(rel, body string) func(map[string][]byte) {
		return func(e map[string][]byte) { e[rel] = []byte(body) }
	}
	cases := map[string]func(map[string][]byte){
		"unknown key":        set("semantics/metrics/net_revenue.yaml", semMetric+"surprise: 1\n"),
		"anchor":             set("semantics/metrics/net_revenue.yaml", strings.Replace(semMetric, "version: 1", "version: &v 1", 1)),
		"name vs file":       set("semantics/metrics/other.yaml", semMetric),
		"hostile measure":    set("semantics/metrics/net_revenue.yaml", strings.Replace(semMetric, "sum(amount)", "1; DROP TABLE x", 1)),
		"oversize yaml":      set("semantics/metrics/net_revenue.yaml", semMetric+"description: "+strings.Repeat("x", semantics.MaxYAMLBytes)+"\n"),
		"control char":       set("semantics/glossary.md", "## a\x1b[2J\n"),
		"oversize glossary":  set("semantics/glossary.md", strings.Repeat("x", semantics.MaxGlossaryBytes+1)),
		"unknown folder":     set("semantics/other/x.yaml", semMetric),
		"nested":             set("semantics/metrics/sub/x.yaml", semMetric),
		"upper-case name":    set("semantics/metrics/Net.yaml", strings.Replace(semMetric, "net_revenue", "Net", 1)),
		"stray file":         set("semantics/notes.md", "x"),
		"yml extension":      set("semantics/metrics/net_revenue.yml", semMetric),
		"a proposal":         set(".slaw-local/proposals/20261029T153012Z-metric-x-7f3a.yaml", "version: 1\n"),
		"proposal in folder": set("semantics/proposals/x.yaml", "version: 1\n"),
		"traversal":          set("semantics/../x.yaml", semMetric),
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			entries := good()
			mutate(entries)
			_, err := Open(build(t, entries, nil), "")
			require.Error(t, err)
			assert.True(t, strings.Contains(err.Error(), "damaged") || strings.Contains(err.Error(), "too large"), err.Error())
		})
	}
	t.Run("the good bundle opens", func(t *testing.T) {
		_, err := Open(build(t, good(), nil), "")
		require.NoError(t, err)
	})
}

func TestSemantics_AProposalIsNeverABundleEntry(t *testing.T) {
	src, _ := newWS(t)
	writeSem(t, src, semFiles)
	writeSem(t, src, map[string]string{
		".slaw-local/proposals/20261029T153012Z-metric-x-7f3a.yaml": "version: 1\nid: x\n",
		"semantics/proposals/y.yaml":                                "version: 1\n",
	})
	res, err := Export(context.Background(), src, ExportOptions{Selection: all()})
	require.NoError(t, err)
	zr, err := zip.NewReader(bytes.NewReader(res.Data), int64(len(res.Data)))
	require.NoError(t, err)
	for _, f := range zr.File {
		assert.NotContains(t, f.Name, "proposals", f.Name)
		assert.False(t, strings.HasPrefix(f.Name, ".slaw-local"), f.Name)
	}
	for _, kind := range []ItemKind{KindNotebook, KindQuery, KindSkill, KindArtifact, KindSemantics} {
		_, _, err := classify(".slaw-local/proposals/x.yaml")
		assert.ErrorIs(t, err, ErrCorrupt, string(kind))
	}
}

func TestSemantics_ImportConflicts(t *testing.T) {
	src, _ := newWS(t)
	writeSem(t, src, semFiles)
	res, err := Export(context.Background(), src, ExportOptions{Selection: all()})
	require.NoError(t, err)
	ctx := context.Background()

	existing := strings.Replace(semMetric, "Net revenue", "Mine", 1)
	setup := func(t *testing.T) workspace.Workspace {
		dst, _ := newWS(t)
		writeSem(t, dst, map[string]string{"semantics/metrics/net_revenue.yaml": existing, "semantics/glossary.md": "## mine\n"})
		return dst
	}

	t.Run("preview marks the clashes", func(t *testing.T) {
		dst := setup(t)
		pkg, err := Open(res.Data, "")
		require.NoError(t, err)
		clashes := map[string]bool{}
		for _, it := range pkg.Preview(ctx, dst).Items {
			clashes[it.Key] = it.Conflict
		}
		assert.True(t, clashes["semantics/metrics/net_revenue"])
		assert.True(t, clashes["semantics/glossary"])
		for _, it := range pkg.Preview(ctx, dst).Items {
			assert.Equal(t, it.Key == "semantics/glossary", it.NoRename, it.Key)
		}
		assert.False(t, clashes["semantics/entities/customer"])
	})

	t.Run("rename keeps both and fixes the name inside", func(t *testing.T) {
		dst := setup(t)
		pkg, err := Open(res.Data, "")
		require.NoError(t, err)
		out, err := pkg.Import(ctx, dst, ImportOptions{})
		require.NoError(t, err)
		byKey := map[string]Outcome{}
		for _, o := range out.Outcomes {
			byKey[o.Key] = o
		}
		assert.Equal(t, "renamed", byKey["semantics/metrics/net_revenue"].Result)
		assert.Equal(t, "metrics/net_revenue_2", byKey["semantics/metrics/net_revenue"].FinalID)
		assert.Equal(t, existing, readSem(t, dst, "semantics/metrics/net_revenue.yaml"))
		m, err := dst.Semantics().Metric(ctx, "net_revenue_2")
		require.NoError(t, err)
		assert.Equal(t, "Net revenue", m.Title)
		// One file cannot be renamed: the default is to skip it, and the existing one stays.
		g := byKey["semantics/glossary"]
		assert.Equal(t, "skipped", g.Result)
		assert.Empty(t, g.Error)
		assert.Equal(t, "## mine\n", readSem(t, dst, "semantics/glossary.md"))
	})

	t.Run("skip", func(t *testing.T) {
		dst := setup(t)
		pkg, err := Open(res.Data, "")
		require.NoError(t, err)
		out, err := pkg.Import(ctx, dst, ImportOptions{OnConflict: ActionSkip})
		require.NoError(t, err)
		require.Zero(t, out.Failed())
		assert.Equal(t, existing, readSem(t, dst, "semantics/metrics/net_revenue.yaml"))
		assert.Equal(t, "## mine\n", readSem(t, dst, "semantics/glossary.md"))
	})

	t.Run("overwrite replaces, byte for byte", func(t *testing.T) {
		dst := setup(t)
		pkg, err := Open(res.Data, "")
		require.NoError(t, err)
		pkg.Preview(ctx, dst) // notes the revisions the overwrite is checked against
		out, err := pkg.Import(ctx, dst, ImportOptions{OnConflict: ActionOverwrite})
		require.NoError(t, err)
		require.Zero(t, out.Failed(), "%+v", out.Outcomes)
		assert.Equal(t, semMetric, readSem(t, dst, "semantics/metrics/net_revenue.yaml"))
		assert.Equal(t, semGlossary, readSem(t, dst, "semantics/glossary.md"))
	})

	t.Run("overwrite is refused if the file changed since the preview", func(t *testing.T) {
		dst := setup(t)
		pkg, err := Open(res.Data, "")
		require.NoError(t, err)
		pkg.Preview(ctx, dst)
		writeSem(t, dst, map[string]string{"semantics/metrics/net_revenue.yaml": existing + "owner: someone\n"})
		out, err := pkg.Import(ctx, dst, ImportOptions{Decisions: map[string]Action{"semantics/metrics/net_revenue": ActionOverwrite}})
		require.NoError(t, err)
		var o Outcome
		for _, x := range out.Outcomes {
			if x.Key == "semantics/metrics/net_revenue" {
				o = x
			}
		}
		assert.Equal(t, "failed", o.Result)
		assert.Contains(t, o.Error, "changed on disk")
		assert.Contains(t, readSem(t, dst, "semantics/metrics/net_revenue.yaml"), "owner: someone")
	})

	t.Run("dry run writes nothing", func(t *testing.T) {
		dst, _ := newWS(t)
		pkg, err := Open(res.Data, "")
		require.NoError(t, err)
		out, err := pkg.Import(ctx, dst, ImportOptions{DryRun: true})
		require.NoError(t, err)
		require.Zero(t, out.Failed())
		_, err = os.Stat(filepath.Join(dst.Root(), "semantics"))
		assert.True(t, os.IsNotExist(err))
	})
}

func TestSemantics_RenamesFollowReferencesInTheSameBundle(t *testing.T) {
	ctx := context.Background()
	src, _ := newWS(t)
	require.NoError(t, src.Queries().Create(ctx, &queries.Query{ID: "net_revenue_base", Title: "Base", SQL: "SELECT 1 AS amount"}))
	writeSem(t, src, semFiles)
	res, err := Export(ctx, src, ExportOptions{Selection: all()})
	require.NoError(t, err)

	// The destination already has its own query, entity and metric of those names.
	dst, _ := newWS(t)
	require.NoError(t, dst.Queries().Create(ctx, &queries.Query{ID: "net_revenue_base", Title: "Mine", SQL: "SELECT 2 AS amount"}))
	writeSem(t, dst, map[string]string{
		"semantics/entities/customer.yaml": strings.Replace(semEntity, "customers", "my_customers", 1),
	})
	pkg, err := Open(res.Data, "")
	require.NoError(t, err)
	out, err := pkg.Import(ctx, dst, ImportOptions{})
	require.NoError(t, err)
	require.Zero(t, out.Failed(), "%+v", out.Outcomes)

	// The query was renamed on import, and the metric that came with it follows.
	byKey := map[string]Outcome{}
	for _, o := range out.Outcomes {
		byKey[o.Key] = o
	}
	newQ := byKey["query/net_revenue_base"].FinalID
	require.NotEqual(t, "net_revenue_base", newQ)
	m, err := dst.Semantics().Metric(ctx, "net_revenue")
	require.NoError(t, err)
	assert.Equal(t, newQ, m.Query, "the imported metric uses the query imported with it, not the workspace's own")
	own, err := dst.Queries().Get(ctx, "net_revenue_base")
	require.NoError(t, err)
	assert.Equal(t, "Mine", own.Title)

	// The entity was renamed, and the join that came with it follows.
	assert.Equal(t, "entities/customer_2", byKey["semantics/entities/customer"].FinalID)
	j, err := dst.Semantics().Join(ctx, "orders_customers")
	require.NoError(t, err)
	assert.Equal(t, "customer_2", j.Right)
	assert.Equal(t, "orders", j.Left, "a reference to something the bundle did not rename is left alone")

	notes := strings.Join(out.Warnings, "\n")
	assert.Contains(t, notes, "metric net_revenue now uses the saved query")
	assert.Contains(t, notes, "join orders_customers now uses the entity")
	// The same notes show on a dry run.
	dry, err := pkg.Import(ctx, dst, ImportOptions{DryRun: true})
	require.NoError(t, err)
	assert.Contains(t, strings.Join(dry.Warnings, "\n"), "metric net_revenue now uses")
}

func TestSemantics_SkippingAClashingQueryOrEntityWarnsThatReferencesNowPointAtTheWorkspaces(t *testing.T) {
	ctx := context.Background()
	src, _ := newWS(t)
	require.NoError(t, src.Queries().Create(ctx, &queries.Query{ID: "net_revenue_base", Title: "Base", SQL: "SELECT 1 AS amount"}))
	writeSem(t, src, semFiles)
	res, err := Export(ctx, src, ExportOptions{Selection: all()})
	require.NoError(t, err)

	dst, _ := newWS(t)
	require.NoError(t, dst.Queries().Create(ctx, &queries.Query{ID: "net_revenue_base", Title: "Mine", SQL: "SELECT 2 AS amount"}))
	writeSem(t, dst, map[string]string{
		"semantics/entities/customer.yaml": strings.Replace(semEntity, "customers", "my_customers", 1),
	})
	pkg, err := Open(res.Data, "")
	require.NoError(t, err)
	out, err := pkg.Import(ctx, dst, ImportOptions{OnConflict: ActionSkip})
	require.NoError(t, err)
	require.Zero(t, out.Failed(), "%+v", out.Outcomes)

	notes := strings.Join(out.Warnings, "\n")
	assert.Contains(t, notes, `metric net_revenue uses the saved query "net_revenue_base", which was skipped`)
	assert.Contains(t, notes, "points at the workspace's")
	assert.Contains(t, notes, `join orders_customers uses the entity "customer", which was skipped`)
	m, err := dst.Semantics().Metric(ctx, "net_revenue")
	require.NoError(t, err)
	assert.Equal(t, "net_revenue_base", m.Query)

	// A skip the person chose for an item that did not clash is not this case: nothing to point at.
	fresh, _ := newWS(t)
	pkg, err = Open(res.Data, "")
	require.NoError(t, err)
	out, err = pkg.Import(ctx, fresh, ImportOptions{})
	require.NoError(t, err)
	assert.NotContains(t, strings.Join(out.Warnings, "\n"), "was skipped")
}
