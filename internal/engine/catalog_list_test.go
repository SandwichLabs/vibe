package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListCatalog_LevelsOfASecondCatalog(t *testing.T) {
	s := hubWithOther(t)
	ctx := context.Background()

	// Only `main`: the schema level is hidden and the tables come straight back.
	lvl, err := ListCatalog(ctx, s, CatalogRequest{Connection: "other"})
	require.NoError(t, err)
	assert.Equal(t, "tables", lvl.Kind)
	assert.True(t, lvl.Flattened)
	assert.Equal(t, "main", lvl.Schema)
	names := map[string]CatalogNode{}
	for _, n := range lvl.Nodes {
		names[n.Name] = n
	}
	require.Contains(t, names, "t")
	assert.Equal(t, "table", names["t"].Kind)
	assert.Equal(t, "other.main.t", names["t"].Qualified)

	// A named schema is not flattened, even when it is main.
	lvl, err = ListCatalog(ctx, s, CatalogRequest{Connection: "other", Schema: "main"})
	require.NoError(t, err)
	assert.False(t, lvl.Flattened)

	// Columns: keys and the schema defaulted.
	cols, err := ListCatalog(ctx, s, CatalogRequest{Connection: "other", Table: "child"})
	require.NoError(t, err)
	assert.Equal(t, "columns", cols.Kind)
	assert.Equal(t, "main", cols.Schema)
	require.Len(t, cols.Nodes, 1)
	assert.True(t, cols.Nodes[0].FK)
	assert.EqualValues(t, -1, cols.Nodes[0].Rows)
}

func TestListCatalog_UnknownConnection(t *testing.T) {
	s := hubWithOther(t)
	for _, name := range []string{`x"; DROP`, ""} {
		_, err := ListCatalog(context.Background(), s, CatalogRequest{Connection: name})
		var unknown *UnknownConnectionError
		require.ErrorAs(t, err, &unknown, name)
		assert.Contains(t, err.Error(), "There is no connection called")
	}
}

func TestRelationKind(t *testing.T) {
	assert.Equal(t, "view", RelationKind("VIEW"))
	assert.Equal(t, "view", RelationKind("materialized view"))
	assert.Equal(t, "table", RelationKind("BASE TABLE"))
	assert.Equal(t, "table", RelationKind(""))
}
