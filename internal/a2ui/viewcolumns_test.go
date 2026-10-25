package a2ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckViewColumns(t *testing.T) {
	vc := ViewContext{Params: []ViewParam{{Name: "who", Type: ParamText}}, Queries: []string{"q", "one"}}
	page := `{"title":"T","components":[
	  {"id":"h","type":"heading","text":"x"},
	  {"id":"p","type":"pivot","query":"q","rows":["region"],"columns":"month","value":"revenue"},
	  {"id":"t","type":"table","query":"q","drill":{"param":"who","column":"customer"}},
	  {"id":"b","type":"barChart","query":"one"},
	  {"id":"ok","type":"barChart","query":"q"}]}`
	p, err := ValidateView(page, vc)
	require.NoError(t, err)

	got := CheckViewColumns(p, map[string][]string{"q": {"region", "month", "revenue"}, "one": {"n"}})
	require.Len(t, got, 2)
	assert.Contains(t, got[0], "components[2].drill.column")
	assert.Contains(t, got[0], `"customer"`)
	assert.Contains(t, got[0], "Columns: region, month, revenue")
	assert.Contains(t, got[1], "components[3]")
	assert.Contains(t, got[1], "barChart")

	got = CheckViewColumns(p, map[string][]string{"q": {"region", "when", "revenue"}})
	require.NotEmpty(t, got)
	assert.Contains(t, got[0], "components[1].columns")
	assert.Contains(t, got[0], `"month"`)

	assert.Empty(t, CheckViewColumns(p, nil), "a query with no known result is skipped")

	s1, err := ValidateView(`{"title":"T","components":[{"id":"s","type":"streamChart","query":"one"}]}`, vc)
	require.NoError(t, err)
	assert.Empty(t, CheckViewColumns(s1, map[string][]string{"one": {"n"}}), "a stream chart reads one column")
}
