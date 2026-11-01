package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SandwichLabs/assay/internal/lsp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompleteSQL_NoWorkspace(t *testing.T) {
	_, err := newBareApp(t).CompleteSQL(CompleteSQLRequest{SQL: "SELECT"})
	assert.ErrorIs(t, err, errNoWorkspace)
}

func TestCompleteSQL_SecondCatalog(t *testing.T) {
	app := appWithSecondCatalog(t)
	// Attaching through the session bypasses Studio's own event.
	app.catalogChanged("other")
	src := "SELECT * FROM other.main."
	var res *CompleteSQLResult
	require.Eventually(t, func() bool {
		var err error
		res, err = app.CompleteSQL(CompleteSQLRequest{SQL: src, Offset: len(src)})
		return err == nil && len(res.Items) > 0
	}, 5*time.Second, 10*time.Millisecond)
	require.Len(t, res.Items, 1)
	assert.Equal(t, "items", res.Items[0].Label)
	assert.Equal(t, "table", res.Items[0].Kind)
	assert.Equal(t, len(src), res.From)
}

func TestCompleteSQL_BadConnectionName(t *testing.T) {
	app := appWithSecondCatalog(t)
	_, err := app.CompleteSQL(CompleteSQLRequest{SQL: "SELECT", Connection: "x;drop"})
	require.Error(t, err)
	assert.Equal(t, `unknown connection "x;drop"`, err.Error())
}

func TestCloseWorkspaceStopsLiveCatalog(t *testing.T) {
	app := appWithSecondCatalog(t)
	require.NotNil(t, app.liveCat.Load())
	require.NoError(t, app.CloseWorkspace())
	assert.Nil(t, app.liveCat.Load())
	_, err := app.CompleteSQL(CompleteSQLRequest{SQL: "SELECT"})
	assert.ErrorIs(t, err, errNoWorkspace)
}

// The completion catalog rebuilds from catalogChanged, so no other code may
// emit catalog:changed on its own.
func TestCatalogChangedIsTheOnlyEmitter(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	n := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		require.NoError(t, err)
		n += strings.Count(string(b), `"catalog:changed"`)
	}
	assert.Equal(t, 1, n, `only catalogChanged may name "catalog:changed"`)
}

func TestToCompleteResult_MapsEveryField(t *testing.T) {
	got := toCompleteResult(lsp.CompleteResult{
		Items:      []lsp.Item{{Label: "orders", Kind: lsp.KindTable, Detail: "d", Insert: `"orders"`}},
		From:       4,
		Stale:      []string{"pg"},
		Building:   true,
		Incomplete: true,
	})
	assert.Equal(t, &CompleteSQLResult{
		From:       4,
		Items:      []CompletionItem{{Label: "orders", Kind: "table", Detail: "d", Insert: `"orders"`}},
		Stale:      []string{"pg"},
		Building:   true,
		Incomplete: true,
	}, got)
	assert.NotNil(t, toCompleteResult(lsp.CompleteResult{}).Items, "an empty list is [], not null")
}
