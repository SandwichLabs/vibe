package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestResultSetNamesAndTypes proves the one shared name/type projection —
// the method studio's TableData/A2UIQueryResult bindings and
// internal/agents' run_query both now call,
// replacing their own duplicated copies of this loop.
func TestResultSetNamesAndTypes(t *testing.T) {
	rs := &ResultSet{Columns: []Column{
		{Name: "id", Type: "BIGINT"},
		{Name: "amt", Type: "DECIMAL(12,2)"},
	}}
	names, types := rs.NamesAndTypes()
	assert.Equal(t, []string{"id", "amt"}, names)
	assert.Equal(t, []string{"BIGINT", "DECIMAL(12,2)"}, types)
}

// TestResultSetNamesAndTypesEmpty proves a zero-column ResultSet returns
// empty (not nil-panicking) slices.
func TestResultSetNamesAndTypesEmpty(t *testing.T) {
	rs := &ResultSet{}
	names, types := rs.NamesAndTypes()
	assert.Empty(t, names)
	assert.Empty(t, types)
}
