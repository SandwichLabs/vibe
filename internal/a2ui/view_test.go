package a2ui

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testCtx = ViewContext{
	Params: []ViewParam{
		{"from", ParamDate}, {"to", ParamDate}, {"region", ParamEnum}, {"active", ParamBoolean},
		{"min_amount", ParamNumber}, {"top", ParamInteger}, {"q", ParamText},
	},
	Queries: []string{"by_region", "customers"},
}

func viewPage(components string) string {
	return `{"title":"T","components":[` + components + `]}`
}

func TestValidateView_NewTypesValid(t *testing.T) {
	page := viewPage(`
	  {"id":"r","type":"container","layout":"row"},
	  {"id":"c1","type":"select","parent":"r","param":"region","label":"Region"},
	  {"id":"c2","type":"select","parent":"r","param":"active"},
	  {"id":"c3","type":"dateRange","parent":"r","start":"from","end":"to","label":"Dates"},
	  {"id":"c4","type":"numberInput","parent":"r","param":"min_amount"},
	  {"id":"c5","type":"numberInput","parent":"r","param":"top"},
	  {"id":"c6","type":"textInput","parent":"r","param":"q"},
	  {"id":"p","type":"pivot","query":"by_region","rows":["region"],"columns":"month","value":"revenue","label":"Revenue","drill":{"param":"region","column":"region"}},
	  {"id":"t","type":"table","query":"customers","drill":{"param":"q","column":"customer"}},
	  {"id":"b","type":"barChart","query":"by_region","color":"yellow","drill":{"param":"region","column":"region"}},
	  {"id":"k","type":"columnChart","query":"by_region","drill":{"param":"region","column":"region"}},
	  {"id":"s","type":"stat","label":"Total","query":"by_region"},
	  {"id":"i","type":"table","columns":["a"],"rows":[["x"]]}`)
	got, err := ValidateView(page, testCtx)
	require.NoError(t, err)
	assert.Len(t, got.Components, 13)
}

func TestValidateView_AcceptsDecodedAndBytes(t *testing.T) {
	_, err := ValidateView([]byte(viewPage(`{"id":"a","type":"heading","text":"x"}`)), testCtx)
	require.NoError(t, err)
	_, err = ValidateView(map[string]any{"title": "T", "components": []any{map[string]any{"id": "a", "type": "heading", "text": "x"}}}, testCtx)
	require.NoError(t, err)
}

func TestValidateView_Errors(t *testing.T) {
	cases := []struct{ name, comp, want string }{
		{"select on text", `{"id":"a","type":"select","param":"q"}`,
			`components[0].param: parameter "q" is type text here, but this field needs enum or boolean`},
		{"select on date", `{"id":"a","type":"select","param":"from"}`,
			`components[0].param: parameter "from" is type date here, but this field needs enum or boolean`},
		{"select missing param", `{"id":"a","type":"select","param":"nope"}`,
			`components[0].param: "nope" is not a declared parameter of this artifact. Parameters: active, from, min_amount, q, region, to, top`},
		{"select no param", `{"id":"a","type":"select"}`, `components[0].param: required for type "select"`},
		{"dateRange over non-date", `{"id":"a","type":"dateRange","start":"from","end":"region"}`,
			`components[0].end: parameter "region" is type enum here, but this field needs date`},
		{"dateRange start missing", `{"id":"a","type":"dateRange","end":"to"}`, `components[0].start: required for type "dateRange"`},
		{"numberInput on text", `{"id":"a","type":"numberInput","param":"q"}`,
			`components[0].param: parameter "q" is type text here, but this field needs number or integer`},
		{"textInput on number", `{"id":"a","type":"textInput","param":"top"}`,
			`components[0].param: parameter "top" is type integer here, but this field needs text`},
		{"param not a string", `{"id":"a","type":"select","param":3}`, `components[0].param: expected a parameter name (string), got number`},
		{"sql refused", `{"id":"a","type":"table","sql":"SELECT 1"}`,
			`components[0].sql: "sql" is not allowed in a data artifact view: name one of the artifact's queries with "query"`},
		{"connection refused", `{"id":"a","type":"barChart","connection":"x"}`,
			`components[0].connection: "connection" is not allowed in a data artifact view`},
		{"refresh on a query-bound chart", `{"id":"a","type":"streamChart","query":"by_region","refresh":10}`,
			`components[0].refresh: "refresh" is not allowed in a data artifact view`},
		{"query missing", `{"id":"a","type":"table","query":"gone"}`,
			`components[0].query: "gone" is not a query of this artifact. Queries: by_region, customers`},
		{"query not string", `{"id":"a","type":"table","query":1}`, `components[0].query: expected a query name (string), got number`},
		{"pivot query missing", `{"id":"a","type":"pivot","query":"gone","rows":["r"],"columns":"c","value":"v"}`,
			`components[0].query: "gone" is not a query of this artifact`},
		{"pivot no rows", `{"id":"a","type":"pivot","query":"by_region","rows":[],"columns":"c","value":"v"}`,
			`components[0].rows: expected 1 to 8 column names, got 0`},
		{"pivot bad row", `{"id":"a","type":"pivot","query":"by_region","rows":[1],"columns":"c","value":"v"}`,
			`components[0].rows[0]: expected a column name (string), got number`},
		{"pivot dup column", `{"id":"a","type":"pivot","query":"by_region","rows":["x"],"columns":"x","value":"v"}`,
			`components[0]: column "x" is used as both a row key and the column key`},
		{"pivot missing value", `{"id":"a","type":"pivot","query":"by_region","rows":["x"],"columns":"c"}`,
			`components[0].value: required for type "pivot"`},
		{"pivot empty name", `{"id":"a","type":"pivot","query":"by_region","rows":["x"],"columns":"","value":"v"}`,
			`components[0].columns: a column name is 1 to 200 characters on one line`},
		{"pivot newline name", `{"id":"a","type":"pivot","query":"by_region","rows":["x"],"columns":"a\nb","value":"v"}`,
			`components[0].columns: a column name is 1 to 200 characters on one line`},
		{"pivot unknown field", `{"id":"a","type":"pivot","query":"by_region","rows":["x"],"columns":"c","value":"v","color":"yellow"}`,
			`components[0].color: "pivot" has no such field. Allowed: columns, drill, id, label, parent, query, rows, type, value`},
		{"drill undeclared", `{"id":"a","type":"table","drill":{"param":"nope","column":"c"}}`,
			`components[0].drill.param: "nope" is not a declared parameter`},
		{"drill no column", `{"id":"a","type":"table","drill":{"param":"region"}}`, `components[0].drill.column: required for drill`},
		{"drill extra key", `{"id":"a","type":"table","drill":{"param":"region","column":"c","sql":"x"}}`,
			`components[0].drill.sql: drill has no such field. Allowed: column, param`},
		{"drill not object", `{"id":"a","type":"table","drill":"region"}`, `components[0].drill: expected an object like {param, column}, got string`},
		{"drill on stat", `{"id":"a","type":"stat","label":"x","drill":{"param":"region","column":"c"}}`,
			`components[0].drill: "stat" has no such field`},
		{"drill on heading", `{"id":"a","type":"heading","text":"x","drill":{"param":"region","column":"c"}}`,
			`components[0].drill: "heading" has no such field`},
		{"select unknown field", `{"id":"a","type":"select","param":"region","options":["x"]}`,
			`components[0].options: "select" has no such field. Allowed: id, label, param, parent, type`},
		{"dateRange same param", `{"id":"a","type":"dateRange","start":"from","end":"from"}`,
			`components[0]: start and end must be two different parameters`},
		{"drill on lineChart", `{"id":"a","type":"lineChart","drill":{"param":"region","column":"c"}}`,
			`components[0].drill: "lineChart" has no such field`},
		{"pivot nine rows", `{"id":"a","type":"pivot","query":"by_region","rows":["a","b","c","d","e","f","g","h","i"],"columns":"x","value":"v"}`,
			`components[0].rows: expected 1 to 8 column names, got 9`},
		{"unknown type", `{"id":"a","type":"slider"}`, `components[0].type: unknown type "slider". Valid types: areaChart,`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateView(viewPage(tc.comp), testCtx)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestValidateView_UnknownTypeListsViewTypes(t *testing.T) {
	_, err := ValidateView(viewPage(`{"id":"a","type":"slider"}`), testCtx)
	require.Error(t, err)
	for name := range ViewComponents {
		assert.Contains(t, err.Error(), name)
	}
}

func TestValidateView_ErrorsAreStable(t *testing.T) {
	page := viewPage(`{"id":"a","type":"table","sql":"x","connection":"c","zz":1,"aa":2,"query":"gone"}`)
	_, first := ValidateView(page, testCtx)
	require.Error(t, first)
	for i := 0; i < 20; i++ {
		_, again := ValidateView(page, testCtx)
		require.EqualError(t, again, first.Error())
	}
}

func TestValidateView_NoParamsOrQueries(t *testing.T) {
	_, err := ValidateView(viewPage(`{"id":"a","type":"select","param":"x"}`), ViewContext{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Parameters: (none)")
	_, err = ValidateView(viewPage(`{"id":"a","type":"table","query":"x"}`), ViewContext{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Queries: (none)")
}

func TestValidateView_SharedStructuralRules(t *testing.T) {
	_, err := ValidateView(viewPage(`{"id":"a","type":"select","param":"region"},{"id":"a","type":"select","param":"region"}`), testCtx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `Duplicate component id: "a"`)
	_, err = ValidateView(`{"title":"","components":[]}`, testCtx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "title: expected a non-empty string")
	assert.Contains(t, err.Error(), "components: must contain at least one component")
}

// Validate (chat, notebooks, create_dashboard) refuses every view-only type
// and field with a pointed message.
func TestValidate_RefusesViewOnly(t *testing.T) {
	for name := range ViewComponents {
		_, err := Validate(viewPage(`{"id":"a","type":"` + name + `"}`))
		require.Error(t, err, name)
		assert.Contains(t, err.Error(), `components[0].type: "`+name+`" is only allowed in a data artifact view`)
	}
	for field, value := range map[string]string{"query": `"q"`, "drill": `{"param":"p","column":"c"}`} {
		_, err := Validate(viewPage(`{"id":"a","type":"table","` + field + `":` + value + `}`))
		require.Error(t, err, field)
		assert.Contains(t, err.Error(), `components[0].`+field+`: "`+field+`" is only allowed in a data artifact view`)
	}
	// Where a view would not take the field either, the generic message stays.
	_, err := Validate(viewPage(`{"id":"a","type":"heading","text":"x","query":"q"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"heading" has no such field`)
}

// A page valid under both schemas means the same thing under both: the
// returned page is identical.
func TestValidateAndValidateView_SamePageSameMeaning(t *testing.T) {
	page := viewPage(`
	  {"id":"h","type":"heading","text":"Hi","level":2},
	  {"id":"r","type":"statRow"},
	  {"id":"s","type":"stat","parent":"r","label":"n","value":3,"color":"error"},
	  {"id":"t","type":"table","columns":["a","b"],"rows":[[1,"x"]],"label":"L"},
	  {"id":"b","type":"barChart","bars":[{"label":"a","value":1}],"motion":{"easing":"expo"}},
	  {"id":"l","type":"list","items":["a"]},{"id":"d","type":"divider"}`)
	chat, err := Validate(page)
	require.NoError(t, err)
	view, err := ValidateView(page, testCtx)
	require.NoError(t, err)
	assert.Equal(t, chat, view)
}

// Every field of a view-only type is one of a few kinds, none of which can
// carry a color, style or class: this fails if someone adds one.
func TestViewComponents_NoStyleCarryingFields(t *testing.T) {
	banned := map[string]bool{"color": true, "style": true, "class": true, "className": true, "css": true, "icon": true}
	allowed := map[fieldKind]bool{kindString: true, kindQuery: true, kindParam: true, kindDrill: true, kindNameList: true, kindColumnName: true}
	for name, spec := range viewSpecs {
		_, viewOnly := ViewComponents[name]
		for _, fields := range []map[string]fieldSpec{spec.req, spec.opt} {
			for field, fs := range fields {
				if !viewOnly {
					continue
				}
				assert.False(t, banned[field], "%s.%s", name, field)
				assert.True(t, allowed[fs.kind], "%s.%s has kind %d", name, field, fs.kind)
			}
		}
	}
	// A hostile column name or label is accepted as text and nothing else: it
	// never changes the shape of the page that comes back.
	hostile := `"><img src=x onerror=alert(1)>{{x}}`
	page, err := ValidateView(viewPage(`{"id":"p","type":"pivot","query":"by_region","label":`+jsonString(hostile)+`,"rows":[`+jsonString(hostile)+`],"columns":"c","value":"v"}`), testCtx)
	require.NoError(t, err)
	assert.Equal(t, hostile, page.Components[0].Fields["label"])
}

func jsonString(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

// The view registry is separate: the chat registry, its order and the
// <a2ui> prompt are unchanged.
func TestViewRegistryIsSeparate(t *testing.T) {
	assert.Len(t, Components, 20)
	for name := range ViewComponents {
		assert.NotContains(t, Components, name)
		assert.NotContains(t, typeOrder, name)
		assert.NotContains(t, PromptBlock(), name)
	}
	assert.Len(t, viewTypeOrder, len(ViewComponents))
	for _, spec := range Components {
		assert.NotContains(t, spec.opt, "query")
		assert.NotContains(t, spec.opt, "drill")
	}
}

func TestPromptBlock_Golden(t *testing.T) {
	want, err := os.ReadFile("testdata/prompt_block.golden")
	require.NoError(t, err)
	assert.Equal(t, string(want), PromptBlock(), "the chat <a2ui> prompt must not change with the artifact view schema")
}

func TestValidateView_MultipleBadDeclaredFieldsAreOrderedStably(t *testing.T) {
	page := viewPage(`{"id":"a","type":"dateRange","start":"region","end":"q"}`)
	_, first := ValidateView(page, testCtx)
	require.Error(t, first)
	assert.Less(t, strings.Index(first.Error(), "components[0].end"), strings.Index(first.Error(), "components[0].start"))
	chat := viewPage(`{"id":"a","type":"stat","label":3,"value":[],"detail":4,"color":"pink"}`)
	_, chatFirst := Validate(chat)
	require.Error(t, chatFirst)
	for i := 0; i < 50; i++ {
		_, again := ValidateView(page, testCtx)
		require.EqualError(t, again, first.Error())
		_, chatAgain := Validate(chat)
		require.EqualError(t, chatAgain, chatFirst.Error())
	}
}
