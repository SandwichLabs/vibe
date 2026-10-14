package a2ui

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// good is the schoolwork-workspace test/unit.mjs "good" fixture, ported
// verbatim.
const goodPageJSON = `{
  "title": "Winter math",
  "components": [
    { "id": "h", "type": "heading", "text": "Winter math", "level": 2 },
    { "id": "row", "type": "statRow" },
    { "id": "s1", "type": "stat", "parent": "row", "label": "Median RIT", "sql": "SELECT median(score) FROM assessment_scores" },
    { "id": "b", "type": "barChart", "label": "By grade", "sql": "SELECT grade_level, AVG(score) FROM students GROUP BY 1", "color": "yellow" },
    { "id": "l", "type": "lineChart", "points": [{ "label": "fall", "value": 201 }, { "label": "winter", "value": 209 }] }
  ]
}`

func TestValidate_ValidPagePasses(t *testing.T) {
	page, err := Validate(goodPageJSON)
	require.NoError(t, err)
	assert.Len(t, page.Components, 5)
	assert.Equal(t, "Winter math", page.Title)
}

func TestValidate_DuplicateIdsRejected(t *testing.T) {
	_, err := Validate(`{"title":"x","components":[
		{"id":"a","type":"text","text":"one"},
		{"id":"a","type":"text","text":"two"}
	]}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `Duplicate component id: "a"`)
}

func TestValidate_DanglingParentRejected(t *testing.T) {
	_, err := Validate(`{"title":"x","components":[{"id":"a","type":"text","text":"hi","parent":"nope"}]}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `non-existent parent "nope"`)
}

func TestValidate_SelfParentRejected(t *testing.T) {
	_, err := Validate(`{"title":"x","components":[{"id":"a","type":"text","text":"hi","parent":"a"}]}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "its own parent")
}

func TestValidate_FreeStringColorRejected(t *testing.T) {
	_, err := Validate(`{"title":"x","components":[
		{"id":"a","type":"badge","text":"hi","color":"red; background:url(javascript:alert(1))"}
	]}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "components[0].color")
}

func TestValidate_UnknownComponentTypeRejected(t *testing.T) {
	_, err := Validate(`{"title":"x","components":[{"id":"a","type":"pieChart"}]}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pieChart")
	assert.Contains(t, err.Error(), "barChart")
	assert.Contains(t, err.Error(), "lineChart")
}

func TestValidate_UnknownFieldRejected(t *testing.T) {
	_, err := Validate(`{"title":"x","components":[{"id":"a","type":"text","text":"hi","onclick":"alert(1)"}]}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "onclick")
}

func TestValidate_MissingRequiredFieldNamedWithPath(t *testing.T) {
	_, err := Validate(`{"title":"x","components":[{"id":"a","type":"heading"}]}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `components[0].text: required`)
}

func TestValidate_NonNumericBarValueRejected(t *testing.T) {
	_, err := Validate(`{"title":"x","components":[
		{"id":"a","type":"barChart","bars":[{"label":"x","value":"not a number"}]}
	]}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "components[0].bars[0].value")
}

func TestValidate_EmptyComponentListRejected(t *testing.T) {
	_, err := Validate(`{"title":"x","components":[]}`)
	assert.Error(t, err)
}

func TestValidate_MissingTitleRejected(t *testing.T) {
	_, err := Validate(`{"components":[{"id":"a","type":"divider"}]}`)
	assert.Error(t, err)
}

func TestValidate_NonObjectRejected(t *testing.T) {
	_, err := Validate(`"nope"`)
	assert.Error(t, err)
}

// Every documented type must actually validate with only its required
// fields — otherwise the a2ui system-prompt block advertises something the
// validator refuses.
func TestValidate_EveryTypeValidatesWithOnlyRequiredFields(t *testing.T) {
	for typeName, spec := range Components {
		t.Run(typeName, func(t *testing.T) {
			comp := map[string]any{"id": "a", "type": typeName}
			for field, fs := range spec.req {
				if fs.kind == kindArray {
					comp[field] = []any{"x"}
				} else {
					comp[field] = "x"
				}
			}
			page := map[string]any{"title": "t", "components": []any{comp}}
			_, err := Validate(page)
			assert.NoError(t, err)
		})
	}
}

// --- Go-specific additions beyond the ported JS suite ---------------------

func TestValidate_TolerantOfDecodedObjectInput(t *testing.T) {
	// A tool-calling shim that hands back an already-parsed object (not a
	// JSON string) for the "page" param must still validate.
	page := map[string]any{
		"title": "x",
		"components": []any{
			map[string]any{"id": "a", "type": "text", "text": "hi"},
		},
	}
	got, err := Validate(page)
	require.NoError(t, err)
	assert.Equal(t, "x", got.Title)
}

func TestValidate_InvalidJSONStringRejected(t *testing.T) {
	_, err := Validate(`{not json`)
	assert.Error(t, err)
}

func TestValidate_ParentCycleRejected(t *testing.T) {
	// a -> b -> c -> a: none of these trip the missing-parent or
	// self-parent checks individually, but the renderer's parent -> child
	// walk would loop forever building this tree.
	_, err := Validate(`{"title":"x","components":[
		{"id":"a","type":"container","parent":"c"},
		{"id":"b","type":"container","parent":"a"},
		{"id":"c","type":"container","parent":"b"}
	]}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parent cycle")
}

func TestValidate_TwoNodeParentCycleRejected(t *testing.T) {
	_, err := Validate(`{"title":"x","components":[
		{"id":"a","type":"container","parent":"b"},
		{"id":"b","type":"container","parent":"a"}
	]}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parent cycle")
}

func TestValidate_LargeAcyclicTreeDoesNotFalselyReportACycle(t *testing.T) {
	// A long, purely acyclic parent chain (a0 -> a1 -> ... -> aN) must not
	// trip the cycle detector or hang.
	var b strings.Builder
	b.WriteString(`{"title":"x","components":[`)
	n := 150
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"id":"a`)
		b.WriteString(strconv.Itoa(i))
		b.WriteString(`","type":"container"`)
		if i > 0 {
			b.WriteString(`,"parent":"a`)
			b.WriteString(strconv.Itoa(i - 1))
			b.WriteString(`"`)
		}
		b.WriteString("}")
	}
	b.WriteString("]}")
	_, err := Validate(b.String())
	assert.NoError(t, err)
}

func TestValidate_ComponentCapEnforced(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"title":"x","components":[`)
	n := maxComponents + 1
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"id":"a`)
		b.WriteString(strconv.Itoa(i))
		b.WriteString(`","type":"divider"}`)
	}
	b.WriteString("]}")
	_, err := Validate(b.String())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeding the maximum")
}

func TestValidate_ComponentCapAllowsExactly200(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"title":"x","components":[`)
	for i := 0; i < maxComponents; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"id":"a`)
		b.WriteString(strconv.Itoa(i))
		b.WriteString(`","type":"divider"}`)
	}
	b.WriteString("]}")
	_, err := Validate(b.String())
	assert.NoError(t, err)
}

func TestValidate_RowsCellTypesAccepted(t *testing.T) {
	page := `{"title":"x","components":[
		{"id":"a","type":"table","columns":["n","v"],"rows":[["a",1],["b",2.5],[null,true]]}
	]}`
	_, err := Validate(page)
	assert.NoError(t, err)
}

func TestValidate_RowsBadCellRejected(t *testing.T) {
	page := `{"title":"x","components":[
		{"id":"a","type":"table","columns":["n"],"rows":[[{"nested":"object"}]]}
	]}`
	_, err := Validate(page)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "components[0].rows[0][0]")
}

func TestValidate_UnknownFieldErrorListsAllowed(t *testing.T) {
	_, err := Validate(`{"title":"x","components":[{"id":"a","type":"badge","text":"hi","style":"color:red"}]}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Allowed:")
}

func TestValidate_HeadingLevelEnum(t *testing.T) {
	_, err := Validate(`{"title":"x","components":[{"id":"a","type":"heading","text":"hi","level":4}]}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "components[0].level")

	_, err = Validate(`{"title":"x","components":[{"id":"a","type":"heading","text":"hi","level":2}]}`)
	assert.NoError(t, err)
}

// TestPromptBlock_MatchesValidator guards against the prompt drifting from
// the schema it's generated from: every type name, its required fields, and
// the color tokens must appear in the rendered block.
func TestPromptBlock_MatchesValidator(t *testing.T) {
	block := PromptBlock()
	assert.True(t, strings.HasPrefix(block, "<a2ui>"))
	assert.True(t, strings.HasSuffix(block, "</a2ui>"))
	for typeName, spec := range Components {
		assert.Contains(t, block, typeName, "type %q missing from prompt block", typeName)
		for field := range spec.req {
			assert.Contains(t, block, field+"(required", "required field %q.%q missing from prompt block", typeName, field)
		}
	}
	for _, c := range ColorTokens {
		assert.Contains(t, block, `"`+c+`"`)
	}
}

func TestValidate_ContainerLayoutEnum(t *testing.T) {
	_, err := Validate(`{"title":"x","components":[{"id":"a","type":"container","layout":"absolute"}]}`)
	require.Error(t, err)

	_, err = Validate(`{"title":"x","components":[{"id":"a","type":"container","layout":"grid","gap":8}]}`)
	assert.NoError(t, err)
}

// Notebook format v2 (#108): data-bound components take an optional
// `connection`; nothing else does, and the value must be a connection name.
func TestValidate_ConnectionOnDataBoundComponents(t *testing.T) {
	for _, typ := range DataBound {
		page, err := Validate(`{"title":"x","components":[{"id":"c","type":"` + typ + `","label":"l","sql":"SELECT 1","connection":"prod_pg"}]}`)
		require.NoError(t, err, typ)
		assert.Equal(t, "prod_pg", page.Components[0].Fields["connection"], typ)
	}
	// Unset stays valid (workspace default).
	_, err := Validate(`{"title":"x","components":[{"id":"c","type":"table","sql":"SELECT 1"}]}`)
	require.NoError(t, err)
}

func TestValidate_ConnectionRejectedOffDataBound(t *testing.T) {
	for _, typ := range []string{"heading", "text", "list", "code", "callout", "badge"} {
		_, err := Validate(`{"title":"x","components":[{"id":"c","type":"` + typ + `","text":"t","items":["a"],"connection":"prod"}]}`)
		require.Error(t, err, typ)
		assert.Contains(t, err.Error(), "connection", typ)
	}
}

func TestValidate_ConnectionMustBeAString(t *testing.T) {
	for _, bad := range []string{`7`, `["a"]`, `{"a":1}`, `true`} {
		_, err := Validate(`{"title":"x","components":[{"id":"c","type":"table","sql":"SELECT 1","connection":` + bad + `}]}`)
		require.Error(t, err, bad)
		assert.Contains(t, err.Error(), "connection", bad)
	}
	// An unknown or odd name still validates: it fails when the cell runs
	// ("unknown connection"), it is never dropped or retargeted at load.
	for _, odd := range []string{`"Prod DB"`, `"gone"`, `""`} {
		page, err := Validate(`{"title":"x","components":[{"id":"c","type":"table","sql":"SELECT 1","connection":` + odd + `}]}`)
		require.NoError(t, err, odd)
		assert.NotNil(t, page.Components[0].Fields["connection"])
	}
}

func TestPromptBlock_MentionsConnection(t *testing.T) {
	assert.Contains(t, PromptBlock(), `"connection"`)
	assert.Contains(t, PromptBlock(), "connection")
}

// KINETIC charts: inline columns + rows (the shape their sql returns) and the
// per-type options validate.
func TestValidate_KineticChartsAccepted(t *testing.T) {
	_, err := Validate(`{"title":"x","components":[
		{"id":"a","type":"areaChart","columns":["hour","primary","baseline"],"rows":[["00:00",28,18],["03:00",44,25]]},
		{"id":"r","type":"ringChart","rows":[["Tensor",92],["VRAM",78,120]],"max":100},
		{"id":"c","type":"columnChart","rows":[["US-East",48,32,16]],"mode":"grouped","sorted":true},
		{"id":"d","type":"radarChart","columns":["axis","current","baseline"],"rows":[["Latency",88,70]],"max":100},
		{"id":"s","type":"sankeyChart","rows":[["Ingest","H100",32]]},
		{"id":"h","type":"heatmap","sql":"SELECT node, hour, load FROM t","connection":"prod"},
		{"id":"t","type":"streamChart","sql":"SELECT ts, v FROM t","refresh":10,"window":30,"threshold":88,"color":"error"}
	]}`)
	require.NoError(t, err)
}

func TestValidate_KineticChartOptionsChecked(t *testing.T) {
	cases := map[string]string{
		`{"id":"c","type":"columnChart","mode":"stack"}`:    "components[0].mode",
		`{"id":"t","type":"streamChart","refresh":1}`:       "components[0].refresh",
		`{"id":"t","type":"streamChart","window":1000}`:     "components[0].window",
		`{"id":"r","type":"ringChart","max":0}`:             "greater than 0",
		`{"id":"a","type":"areaChart","color":"yellow"}`:    `"areaChart" has no such field`,
		`{"id":"a","type":"heatmap","bars":[]}`:             `"heatmap" has no such field`,
		`{"id":"d","type":"radarChart","rows":[[{"a":1}]]}`: "components[0].rows[0][0]",
	}
	for comp, want := range cases {
		_, err := Validate(`{"title":"x","components":[` + comp + `]}`)
		require.Error(t, err, comp)
		assert.Contains(t, err.Error(), want, comp)
	}
}

func TestValidate_MotionOnEveryChart(t *testing.T) {
	for _, typ := range Charts {
		_, err := Validate(`{"title":"x","components":[{"id":"c","type":"` + typ + `","motion":{"easing":"custom","speed":1.5,"stagger":false,"stiffness":190,"damping":12,"mass":1}}]}`)
		require.NoError(t, err, typ)
	}
	for _, typ := range []string{"table", "stat", "text"} {
		_, err := Validate(`{"title":"x","components":[{"id":"c","type":"` + typ + `","label":"l","text":"t","motion":{}}]}`)
		require.Error(t, err, typ)
		assert.Contains(t, err.Error(), "motion", typ)
	}
}

func TestValidate_MotionClosed(t *testing.T) {
	cases := map[string]string{
		`"fast"`:                       "expected an object",
		`{"easing":"bounce"}`:          "motion.easing: expected one of",
		`{"speed":10}`:                 "motion.speed: 10 is out of range (must be 0.25 to 4)",
		`{"mass":0}`:                   "motion.mass",
		`{"stagger":"yes"}`:            "motion.stagger: expected boolean",
		`{"duration":"1s; color:red"}`: "motion.duration: motion has no such field",
	}
	for motion, want := range cases {
		_, err := Validate(`{"title":"x","components":[{"id":"c","type":"barChart","motion":` + motion + `}]}`)
		require.Error(t, err, motion)
		assert.Contains(t, err.Error(), want, motion)
	}
	assert.NoError(t, ValidateMotion(map[string]any{"easing": "steps", "speed": 2.0}))
	assert.Error(t, ValidateMotion(map[string]any{"easing": "nope"}))
}

func TestPromptBlock_DocumentsMotionAndShapes(t *testing.T) {
	block := PromptBlock()
	for _, e := range Easings {
		assert.Contains(t, block, `"`+e+`"`)
	}
	for _, want := range []string{"motion({easing, speed, stagger", "sankeyChart: source, target, weight", "refresh(number 5-3600)", "mode(stacked|grouped)"} {
		assert.Contains(t, block, want)
	}
}
