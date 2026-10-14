package templates

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListIsSortedAndComplete(t *testing.T) {
	assert.Equal(t, []string{"contacts", "inventory", "order-entry", "training-registrations"}, List())
}

func TestEveryTemplateSeedsIntoDuckDB(t *testing.T) {
	for _, name := range List() {
		t.Run(name, func(t *testing.T) {
			tmpl, ok := Get(name)
			require.True(t, ok)
			desc, ok := Description(name)
			require.True(t, ok)
			assert.NotEmpty(t, desc)
			assert.NotEmpty(t, tmpl.Book.Cells, "a starter notebook")

			db, err := sql.Open("duckdb", "")
			require.NoError(t, err)
			defer db.Close() //nolint:errcheck
			err = SeedTables(context.Background(), func(ctx context.Context, q string) error {
				_, err := db.ExecContext(ctx, q)
				return err
			}, tmpl)
			require.NoError(t, err)
			for _, tb := range tmpl.Tables {
				var n int
				require.NoError(t, db.QueryRow("SELECT count(*) FROM "+tb.Name).Scan(&n))
				assert.Greater(t, n, 0, tb.Name)
			}
		})
	}
}

func TestGetUnknown(t *testing.T) {
	_, ok := Get("nope")
	assert.False(t, ok)
	_, ok = Description("nope")
	assert.False(t, ok)
}
