package schema_test

import (
	"path/filepath"
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/sandwich-labs/vibe-app/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRelationshipStoreRoundTrip exercises the full P2.2 acceptance behavior
// end-to-end against a real .vibe container (SQLCipher + DuckDB):
//   - the additive _vibe_relationships migration creates the table
//   - Save stores a relationship and Get/List read it back with all fields
//     round-tripping
//   - ListForTable filters by parent OR child table
//   - Save upserts (update on_delete) without creating duplicates
//   - Delete removes it
//   - definitions persist across Save + Close + reopen
func TestRelationshipStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "relationships.vibe")

	c, err := container.Create(path,
		container.WithPassphrase(testPassphrase),
		container.WithLogger(testLogger()),
		container.WithTitle("Relationships Test"),
	)
	require.NoError(t, err)

	// Migration creates _vibe_relationships in the SQLCipher container.
	require.True(t, tableExists(t, c, "_vibe_relationships"),
		"_vibe_relationships table should exist after migration")

	store := schema.NewRelationshipStore(c.SQLiteDB())

	// customers.id <- orders.customer_id, ON DELETE RESTRICT.
	rel := schema.Relationship{
		ID:               "rel_orders_customer",
		ParentTable:      "customers",
		ParentColumn:     "id",
		ChildTable:       "orders",
		ChildColumn:      "customer_id",
		OnDelete:         "RESTRICT",
		OnUpdate:         "NO ACTION",
		EnforceIntegrity: true,
	}
	require.NoError(t, store.Save(rel))

	got, err := store.Get("rel_orders_customer")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "rel_orders_customer", got.ID)
	assert.Equal(t, "customers", got.ParentTable)
	assert.Equal(t, "id", got.ParentColumn)
	assert.Equal(t, "orders", got.ChildTable)
	assert.Equal(t, "customer_id", got.ChildColumn)
	assert.Equal(t, "RESTRICT", got.OnDelete)
	assert.Equal(t, "NO ACTION", got.OnUpdate)
	assert.True(t, got.EnforceIntegrity)
	assert.NotEmpty(t, got.CreatedAt, "created_at should be populated on save")

	// List returns the single relationship.
	all, err := store.List()
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, "rel_orders_customer", all[0].ID)

	// ListForTable filters by parent table...
	byParent, err := store.ListForTable("customers")
	require.NoError(t, err)
	require.Len(t, byParent, 1, "ListForTable should match on parent_table")
	assert.Equal(t, "rel_orders_customer", byParent[0].ID)

	// ...and by child table.
	byChild, err := store.ListForTable("orders")
	require.NoError(t, err)
	require.Len(t, byChild, 1, "ListForTable should match on child_table")
	assert.Equal(t, "rel_orders_customer", byChild[0].ID)

	// ...and excludes unrelated tables.
	none, err := store.ListForTable("products")
	require.NoError(t, err)
	assert.Empty(t, none, "ListForTable should not match unrelated tables")

	// Upsert: change on_delete to CASCADE without creating a duplicate row.
	rel2 := rel
	rel2.OnDelete = "CASCADE"
	require.NoError(t, store.Save(rel2))

	after, err := store.List()
	require.NoError(t, err)
	require.Len(t, after, 1, "upsert should not create a duplicate row")
	assert.Equal(t, "CASCADE", after[0].OnDelete)

	// Persist across Save + Close + reopen.
	require.NoError(t, c.Save())
	require.NoError(t, c.Close())

	c2, err := container.Open(path,
		container.WithPassphrase(testPassphrase),
		container.WithLogger(testLogger()),
	)
	require.NoError(t, err)
	defer c2.Close() //nolint:errcheck

	store2 := schema.NewRelationshipStore(c2.SQLiteDB())
	reopened, err := store2.Get("rel_orders_customer")
	require.NoError(t, err)
	require.NotNil(t, reopened)
	assert.Equal(t, "CASCADE", reopened.OnDelete, "relationship must persist across reopen")
	assert.Equal(t, "orders", reopened.ChildTable)
	assert.Equal(t, "customer_id", reopened.ChildColumn)
	assert.True(t, reopened.EnforceIntegrity)

	// Delete removes it.
	require.NoError(t, store2.Delete("rel_orders_customer"))
	gone, err := store2.List()
	require.NoError(t, err)
	assert.Empty(t, gone, "relationship should be gone after delete")
}
