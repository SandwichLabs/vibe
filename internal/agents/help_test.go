package agents

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchHelpEmptyQuery(t *testing.T) {
	results := SearchHelp("")
	assert.Equal(t, len(HelpEntries), len(results))
}

func TestSearchHelpByKeyword(t *testing.T) {
	results := SearchHelp("chart")
	require.NotEmpty(t, results)
	// The chart directive topic should rank highly.
	assert.Equal(t, "directive-chart", results[0].Topic)
}

func TestSearchHelpMultipleTerms(t *testing.T) {
	results := SearchHelp("go template range")
	require.NotEmpty(t, results)
	assert.Equal(t, "go-templates", results[0].Topic)
}

func TestSearchHelpNoMatch(t *testing.T) {
	results := SearchHelp("xyznonexistent")
	assert.Empty(t, results)
}

func TestSearchHelpSQL(t *testing.T) {
	results := SearchHelp("sql block engine")
	require.NotEmpty(t, results)
	assert.Equal(t, "sql-blocks", results[0].Topic)
}

func TestSearchHelpFrontmatter(t *testing.T) {
	results := SearchHelp("frontmatter params yaml")
	require.NotEmpty(t, results)
	assert.Equal(t, "frontmatter", results[0].Topic)
}
