package connections

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSQLiteTableExpressionDetection(t *testing.T) {
	for _, honest := range []string{
		`CREATE TABLE ducklake_snapshot(snapshot_id BIGINT PRIMARY KEY, snapshot_time VARCHAR)`,
		`CREATE TABLE ducklake_metadata("key" VARCHAR NOT NULL, "value" VARCHAR NOT NULL)`,
		`CREATE TABLE ducklake_inlined_data_1_1(row_id BIGINT, "default" BIGINT, "check" VARCHAR, "as" VARCHAR, default_value VARCHAR)`,
	} {
		assert.False(t, sqliteTableHasExpression(honest), honest)
	}
	for _, bad := range []string{
		`CREATE TABLE t(a VARCHAR DEFAULT 'x')`,
		`CREATE TABLE t(a VARCHAR GENERATED ALWAYS AS (1))`,
		`CREATE TABLE t(a VARCHAR AS (1))`,
		`CREATE TABLE t(a INT CHECK (a > 0))`,
		`CREATE TABLE t(a INT UNIQUE)`,
		`CREATE TABLE t(a INT REFERENCES u(a))`,
		`CREATE VIRTUAL TABLE t USING fts5(a)`,
	} {
		assert.True(t, sqliteTableHasExpression(bad), bad)
	}
}

func TestCatalogRowPatterns(t *testing.T) {
	for _, ok := range []string{"ducklake_inlined_data_1_1", "ducklake_inlined_data_12_345"} {
		assert.True(t, inlinedNameRE.MatchString(ok), ok)
	}
	for _, bad := range []string{"ducklake_inlined_data_1_1 --", "ducklake_inlined_data_1_1\n", "x ducklake_inlined_data_1_1", "ducklake_inlined_data_a_1", "ducklake_metadata", "ducklake_inlined_delete_1"} {
		assert.False(t, inlinedNameRE.MatchString(bad), bad)
	}
	for _, ok := range []string{"identity", "year", "month", "day", "hour", "bucket(4)"} {
		assert.True(t, transformRE.MatchString(ok), ok)
	}
	for _, bad := range []string{"identity(x)", "bucket(4) FROM x", "year\n", "bucket()", ""} {
		assert.False(t, transformRE.MatchString(bad), bad)
	}
}
