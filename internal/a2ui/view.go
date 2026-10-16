package a2ui

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/SandwichLabs/vibe/internal/jsonenc"
)

// This file is the data artifact view schema (docs/plans/data-artifacts.md,
// D6). A data artifact's view is an A2UI page validated against its manifest:
// data comes only from the manifest's NAMED queries (`query`, never `sql`),
// and the new controls and the pivot are available. Validate (chat,
// notebooks, create_dashboard) is unchanged and refuses all of it.
//
// Nothing here adds a color, style or class field. Every view-only field is a
// name checked against the manifest or a plain string the renderer shows as
// text.

// Parameter type names a ViewContext carries. They mirror the manifest's
// (internal/artifacts), which a2ui does not import; a test in that package
// pins the two lists together.
const (
	ParamText    = "text"
	ParamNumber  = "number"
	ParamInteger = "integer"
	ParamDate    = "date"
	ParamBoolean = "boolean"
	ParamEnum    = "enum"
)

// ViewParam is one manifest parameter as the view validator sees it.
type ViewParam struct {
	Name string
	Type string
}

// ViewContext is what ValidateView needs from the manifest: its parameters
// (name and type) and the names of its queries. Plain data, so a2ui imports
// nothing from the artifact package.
type ViewContext struct {
	Params  []ViewParam
	Queries []string
}

// Limits on the new fields. Column names only ever render as text.
const (
	maxPivotRows  = 8
	maxColumnName = 200
)

var (
	querySpec = fieldSpec{kind: kindQuery}
	drillSpec = fieldSpec{kind: kindDrill}
	nameList  = fieldSpec{kind: kindNameList}
	colSpec   = fieldSpec{kind: kindColumnName}

	paramEnumBool = fieldSpec{kind: kindParam, values: []any{ParamEnum, ParamBoolean}}
	paramNumeric  = fieldSpec{kind: kindParam, values: []any{ParamNumber, ParamInteger}}
	paramTextOnly = fieldSpec{kind: kindParam, values: []any{ParamText}}
	paramDateOnly = fieldSpec{kind: kindParam, values: []any{ParamDate}}
)

// ViewComponents is the closed set of component types only a data artifact
// view may use. It is a separate registry from Components on purpose: the
// chat <a2ui> prompt, typeOrder and every `range Components` stay as they
// were, and Validate refuses these types.
var ViewComponents = map[string]componentSpec{
	"select":      {req: map[string]fieldSpec{"param": paramEnumBool}, opt: map[string]fieldSpec{"label": strSpec}},
	"dateRange":   {req: map[string]fieldSpec{"start": paramDateOnly, "end": paramDateOnly}, opt: map[string]fieldSpec{"label": strSpec}},
	"numberInput": {req: map[string]fieldSpec{"param": paramNumeric}, opt: map[string]fieldSpec{"label": strSpec}},
	"textInput":   {req: map[string]fieldSpec{"param": paramTextOnly}, opt: map[string]fieldSpec{"label": strSpec}},
	"pivot": {
		req: map[string]fieldSpec{"query": querySpec, "rows": nameList, "columns": colSpec, "value": colSpec},
		opt: map[string]fieldSpec{"label": strSpec, "drill": drillSpec},
	},
}

// viewTypeOrder is the reading order of ViewComponents (docs and errors).
var viewTypeOrder = []string{"select", "dateRange", "numberInput", "textInput", "pivot"}

// drillable lists the Components types that take `drill` in a view (pivot
// carries it in its own spec).
var drillable = []string{"table", "barChart", "columnChart"}

// viewSpecs is the full type table of a view: Components with sql and
// connection replaced by query (and drill where it applies), plus
// ViewComponents. Built at init from the two registries so it cannot drift.
var viewSpecs map[string]componentSpec

// viewTypeNames is viewSpecs' keys, sorted, for the "Valid types" message.
var viewTypeNames []string

func init() {
	if len(viewTypeOrder) != len(ViewComponents) {
		panic("a2ui: viewTypeOrder is out of sync with ViewComponents")
	}
	for _, t := range viewTypeOrder {
		if _, ok := ViewComponents[t]; !ok {
			panic("a2ui: viewTypeOrder references unknown type " + t)
		}
		if _, clash := Components[t]; clash {
			panic("a2ui: view type " + t + " is also a chat component")
		}
	}
	viewSpecs = make(map[string]componentSpec, len(Components)+len(ViewComponents))
	for name, spec := range Components {
		viewSpecs[name] = viewVariant(name, spec)
	}
	for name, spec := range ViewComponents {
		viewSpecs[name] = spec
	}
	for name := range viewSpecs {
		viewTypeNames = append(viewTypeNames, name)
	}
	sort.Strings(viewTypeNames)
}

// viewVariant copies a chat component's spec for use in a view: a data-bound
// type loses sql and connection and gains query; a drillable one gains drill.
func viewVariant(name string, spec componentSpec) componentSpec {
	out := componentSpec{req: spec.req, opt: map[string]fieldSpec{}}
	for k, v := range spec.opt {
		out.opt[k] = v
	}
	if isDataBound(name) {
		delete(out.opt, "sql")
		delete(out.opt, "connection")
		out.opt["query"] = querySpec
	}
	for _, d := range drillable {
		if d == name {
			out.opt["drill"] = drillSpec
		}
	}
	return out
}

func isDataBound(typeName string) bool {
	for _, d := range DataBound {
		if d == typeName {
			return true
		}
	}
	return false
}

// viewSpecHasField reports whether a view's spec for typeName defines field.
func viewSpecHasField(typeName, field string) bool {
	spec, ok := viewSpecs[typeName]
	if !ok {
		return false
	}
	_, inReq := spec.req[field]
	_, inOpt := spec.opt[field]
	return inReq || inOpt
}

// ValidateView validates raw as a data artifact view against the manifest's
// parameters and queries. raw takes the same shapes as Validate and failures
// use the same newline-joined, JSON-path-prefixed error format, so
// write_artifact can feed them to the model unchanged.
func ValidateView(raw any, vc ViewContext) (Page, error) {
	data, err := decode(raw)
	if err != nil {
		return Page{}, err
	}
	return validateDecoded(data, &vc)
}

// checkViewValue validates one field value. The view-only kinds need the
// ViewContext; every other kind is checkValue's. vc is nil outside a view, and
// then no view-only kind can occur (the specs that carry them are only used
// with a context).
func checkViewValue(vc *ViewContext, spec fieldSpec, value any, path string, errs *[]string) {
	switch spec.kind {
	case kindQuery:
		checkQueryName(vc, value, path, errs)
	case kindParam:
		checkParamName(vc, spec, value, path, errs)
	case kindDrill:
		checkDrill(vc, value, path, errs)
	case kindNameList:
		checkNameList(value, path, errs)
	case kindColumnName:
		checkColumnName(value, path, errs)
	default:
		checkValue(spec, value, path, errs)
	}
}

func checkQueryName(vc *ViewContext, value any, path string, errs *[]string) {
	name, ok := value.(string)
	if !ok {
		*errs = append(*errs, fmt.Sprintf("%s: expected a query name (string), got %s", path, typeName(value)))
		return
	}
	for _, q := range vc.Queries {
		if q == name {
			return
		}
	}
	*errs = append(*errs, fmt.Sprintf("%s: %s is not a query of this artifact. Queries: %s", path, jsonenc.Quote(name), listOrNone(vc.Queries)))
}

func checkParamName(vc *ViewContext, spec fieldSpec, value any, path string, errs *[]string) {
	name, ok := value.(string)
	if !ok {
		*errs = append(*errs, fmt.Sprintf("%s: expected a parameter name (string), got %s", path, typeName(value)))
		return
	}
	got, declared := paramType(vc, name)
	if !declared {
		*errs = append(*errs, fmt.Sprintf("%s: %s is not a declared parameter of this artifact. Parameters: %s", path, jsonenc.Quote(name), listOrNone(paramNames(vc))))
		return
	}
	if len(spec.values) > 0 && !enumContains(spec.values, got) {
		want := make([]string, len(spec.values))
		for i, v := range spec.values {
			want[i], _ = v.(string)
		}
		*errs = append(*errs, fmt.Sprintf("%s: parameter %s is type %s here, but this field needs %s", path, jsonenc.Quote(name), got, strings.Join(want, " or ")))
	}
}

// checkDrill validates {param, column}: param any declared parameter, column a
// result column name (checked against the result at render, since columns are
// only known after a run).
func checkDrill(vc *ViewContext, value any, path string, errs *[]string) {
	obj, ok := value.(map[string]any)
	if !ok {
		*errs = append(*errs, fmt.Sprintf("%s: expected an object like {param, column}, got %s", path, typeName(value)))
		return
	}
	for _, key := range sortedKeys(obj) {
		switch key {
		case "param":
			checkParamName(vc, fieldSpec{kind: kindParam}, obj[key], path+".param", errs)
		case "column":
			checkColumnName(obj[key], path+".column", errs)
		default:
			*errs = append(*errs, fmt.Sprintf("%s.%s: drill has no such field. Allowed: column, param", path, key))
		}
	}
	for _, req := range []string{"column", "param"} {
		if _, present := obj[req]; !present {
			*errs = append(*errs, fmt.Sprintf("%s.%s: required for drill", path, req))
		}
	}
}

func checkNameList(value any, path string, errs *[]string) {
	arr, ok := value.([]any)
	if !ok {
		*errs = append(*errs, fmt.Sprintf("%s: expected an array of column names, got %s", path, typeName(value)))
		return
	}
	if len(arr) == 0 || len(arr) > maxPivotRows {
		*errs = append(*errs, fmt.Sprintf("%s: expected 1 to %d column names, got %d", path, maxPivotRows, len(arr)))
	}
	for i, item := range arr {
		checkColumnName(item, fmt.Sprintf("%s[%d]", path, i), errs)
	}
}

// checkColumnName: a non-empty, valid-UTF-8, single-line name. Column names
// are rendered as text only and matched against the result's column names;
// they never become a class, an id or a style.
func checkColumnName(value any, path string, errs *[]string) {
	s, ok := value.(string)
	switch {
	case !ok:
		*errs = append(*errs, fmt.Sprintf("%s: expected a column name (string), got %s", path, typeName(value)))
	case s == "" || utf8.RuneCountInString(s) > maxColumnName || !utf8.ValidString(s) || strings.ContainsAny(s, "\n\r\x00"):
		*errs = append(*errs, fmt.Sprintf("%s: a column name is 1 to %d characters on one line", path, maxColumnName))
	}
}

// checkPivotColumns refuses a pivot that uses one column for two jobs, which
// could only ever lay out a nonsense grid.
func checkDateRange(obj map[string]any, path string, errs *[]string) {
	if s, ok := obj["start"].(string); ok && s == obj["end"] {
		*errs = append(*errs, fmt.Sprintf("%s: start and end must be two different parameters", path))
	}
}

func checkPivotColumns(obj map[string]any, path string, errs *[]string) {
	seen := map[string]string{}
	use := func(v any, role string) {
		s, ok := v.(string)
		if !ok {
			return
		}
		if prev, dup := seen[s]; dup {
			*errs = append(*errs, fmt.Sprintf("%s: column %s is used as both %s and %s", path, jsonenc.Quote(s), prev, role))
			return
		}
		seen[s] = role
	}
	if rows, ok := obj["rows"].([]any); ok {
		for _, r := range rows {
			use(r, "a row key")
		}
	}
	use(obj["columns"], "the column key")
	use(obj["value"], "the value")
}

func paramType(vc *ViewContext, name string) (string, bool) {
	for _, p := range vc.Params {
		if p.Name == name {
			return p.Type, true
		}
	}
	return "", false
}

func paramNames(vc *ViewContext) []string {
	out := make([]string, len(vc.Params))
	for i, p := range vc.Params {
		out[i] = p.Name
	}
	return out
}

func listOrNone(names []string) string {
	if len(names) == 0 {
		return "(none)"
	}
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	return strings.Join(sorted, ", ")
}
