// Package a2ui implements the A2UI page schema: the closed, validated
// contract an agent uses to hand a chart/table/stat page back to the Studio
// UI. It is a hand-rolled port of schoolwork-workspace's
// src/a2ui/schema.js — same thirteen component types, same closed color
// enum, same structural checks, same newline-joined JSON-path-prefixed error
// format, because that format is fed straight back to the model as a tool
// error for self-correction and its shape is part of the contract, not an
// implementation detail.
//
// Rendering does not happen here (#67 scope: Go validates, Vue renders).
// Colors are a closed enum, never a free string: components are authored by
// a language model, and letting model output reach a style attribute is a
// CSS-injection vector — see CLAUDE.md "Model output never reaches a style
// attribute".
package a2ui

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// ColorTokens is the closed enum of A2UI color tokens. Any other value is a
// validation error — never passed through to a style attribute.
var ColorTokens = []string{"yellow", "black", "error", "muted"}

// DataBound lists the component types whose "sql" field the Vue renderer
// binds live, re-running the query on every render.
var DataBound = []string{"stat", "table", "barChart", "lineChart"}

// maxComponents caps a page at a sane size — a model that goes off the rails
// and emits thousands of components would otherwise hang the renderer and
// bloat the notebook cell it gets saved into.
const maxComponents = 200

// fieldKind is the shape of one field's expected value.
type fieldKind int

const (
	kindString fieldKind = iota
	kindConnection
	kindNumber
	kindBoolean
	kindStringOrNumber
	kindEnum
	kindArray
	kindPoint
	kindRows
)

// fieldSpec describes one component field's expected shape.
type fieldSpec struct {
	kind   fieldKind
	values []any      // kindEnum
	of     *fieldSpec // kindArray
}

var (
	strSpec        = fieldSpec{kind: kindString}
	connSpec       = fieldSpec{kind: kindConnection}
	numSpec        = fieldSpec{kind: kindNumber}
	boolSpec       = fieldSpec{kind: kindBoolean}
	strOrNumSpec   = fieldSpec{kind: kindStringOrNumber}
	colorSpec      = fieldSpec{kind: kindEnum, values: toAny(ColorTokens)}
	headingLvlSpec = fieldSpec{kind: kindEnum, values: []any{float64(1), float64(2), float64(3)}}
	layoutSpec     = fieldSpec{kind: kindEnum, values: []any{"row", "column", "grid"}}
	strArraySpec   = fieldSpec{kind: kindArray, of: &strSpec}
	pointArraySpec = fieldSpec{kind: kindArray, of: &fieldSpec{kind: kindPoint}}
	rowsSpec       = fieldSpec{kind: kindRows}
)

func toAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// componentSpec is one component type's required and optional fields.
type componentSpec struct {
	req map[string]fieldSpec
	opt map[string]fieldSpec
}

// Components is the closed set of the thirteen A2UI component types and
// their field specs, mirroring schema.js's COMPONENTS exactly.
var Components = map[string]componentSpec{
	"heading":   {req: map[string]fieldSpec{"text": strSpec}, opt: map[string]fieldSpec{"level": headingLvlSpec}},
	"text":      {req: map[string]fieldSpec{"text": strSpec}, opt: map[string]fieldSpec{"muted": boolSpec}},
	"divider":   {req: map[string]fieldSpec{}, opt: map[string]fieldSpec{}},
	"stat":      {req: map[string]fieldSpec{"label": strSpec}, opt: map[string]fieldSpec{"value": strOrNumSpec, "sql": strSpec, "connection": connSpec, "detail": strSpec, "color": colorSpec}},
	"statRow":   {req: map[string]fieldSpec{}, opt: map[string]fieldSpec{}},
	"table":     {req: map[string]fieldSpec{}, opt: map[string]fieldSpec{"columns": strArraySpec, "rows": rowsSpec, "sql": strSpec, "connection": connSpec, "label": strSpec}},
	"barChart":  {req: map[string]fieldSpec{}, opt: map[string]fieldSpec{"bars": pointArraySpec, "sql": strSpec, "connection": connSpec, "label": strSpec, "color": colorSpec}},
	"lineChart": {req: map[string]fieldSpec{}, opt: map[string]fieldSpec{"points": pointArraySpec, "sql": strSpec, "connection": connSpec, "label": strSpec, "color": colorSpec}},
	"list":      {req: map[string]fieldSpec{"items": strArraySpec}, opt: map[string]fieldSpec{"label": strSpec, "color": colorSpec}},
	"code":      {req: map[string]fieldSpec{"text": strSpec}, opt: map[string]fieldSpec{"language": strSpec}},
	"callout":   {req: map[string]fieldSpec{"text": strSpec}, opt: map[string]fieldSpec{"icon": strSpec, "color": colorSpec}},
	"badge":     {req: map[string]fieldSpec{"text": strSpec}, opt: map[string]fieldSpec{"color": colorSpec}},
	"container": {req: map[string]fieldSpec{}, opt: map[string]fieldSpec{"layout": layoutSpec, "gap": numSpec}},
}

// componentTypeNames is Components' keys in a stable order, for error
// messages ("Valid types: ...") so output (and therefore tests) is
// deterministic.
var componentTypeNames = func() []string {
	names := make([]string, 0, len(Components))
	for k := range Components {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}()

// Component is one validated A2UI component: the fixed id/type/parent plus
// every other field the model sent, held generically since the field set
// varies by type.
type Component struct {
	ID     string
	Type   string
	Parent string // "" means no parent (top-level)
	Fields map[string]any
}

// MarshalJSON re-flattens Fields alongside id/type/parent, so a validated
// Component round-trips to the same shape the model sent (minus unknown
// fields, which validation already rejected).
func (c Component) MarshalJSON() ([]byte, error) {
	out := make(map[string]any, len(c.Fields)+3)
	for k, v := range c.Fields {
		out[k] = v
	}
	out["id"] = c.ID
	out["type"] = c.Type
	if c.Parent != "" {
		out["parent"] = c.Parent
	}
	return json.Marshal(out)
}

// Page is a validated A2UI page: a non-empty title plus a flat list of
// components nested by parent id.
type Page struct {
	Title      string
	Components []Component
}

// MarshalJSON renders {"title", "components"} — the wire shape the Vue
// renderer and the notebook cell store expect.
func (p Page) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Title      string      `json:"title"`
		Components []Component `json:"components"`
	}{Title: p.Title, Components: p.Components})
}

// typeName mirrors schema.js's typeName: the JSON-ish type name used in
// error messages.
func typeName(v any) string {
	switch vv := v.(type) {
	case nil:
		return "null"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	case string:
		return "string"
	case bool:
		return "boolean"
	case float64, int, int64:
		return "number"
	default:
		_ = vv
		return "object"
	}
}

func jsonQuote(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

// checkValue validates one field value against its spec, appending to errs.
func checkValue(spec fieldSpec, value any, path string, errs *[]string) {
	switch spec.kind {
	case kindString:
		if _, ok := value.(string); !ok {
			*errs = append(*errs, fmt.Sprintf("%s: expected string, got %s", path, typeName(value)))
		}
	case kindConnection:
		// Only the shape is checked here. A name that is not a known connection
		// is a run-time error ("unknown connection"), never a reason to drop
		// the cell or retarget the query silently.
		if _, ok := value.(string); !ok {
			*errs = append(*errs, fmt.Sprintf("%s: expected string, got %s", path, typeName(value)))
		}
	case kindNumber:
		if !isFiniteNumber(value) {
			*errs = append(*errs, fmt.Sprintf("%s: expected a finite number, got %s", path, typeName(value)))
		}
	case kindBoolean:
		if _, ok := value.(bool); !ok {
			*errs = append(*errs, fmt.Sprintf("%s: expected boolean, got %s", path, typeName(value)))
		}
	case kindStringOrNumber:
		_, isStr := value.(string)
		if !isStr && !isFiniteNumber(value) {
			*errs = append(*errs, fmt.Sprintf("%s: expected string or number, got %s", path, typeName(value)))
		}
	case kindEnum:
		if !enumContains(spec.values, value) {
			labels := make([]string, len(spec.values))
			for i, v := range spec.values {
				labels[i] = jsonQuote(v)
			}
			*errs = append(*errs, fmt.Sprintf("%s: expected one of %s, got %s", path, strings.Join(labels, ", "), jsonQuote(value)))
		}
	case kindArray:
		arr, ok := value.([]any)
		if !ok {
			*errs = append(*errs, fmt.Sprintf("%s: expected array, got %s", path, typeName(value)))
			return
		}
		for i, item := range arr {
			checkValue(*spec.of, item, fmt.Sprintf("%s[%d]", path, i), errs)
		}
	case kindPoint:
		obj, ok := value.(map[string]any)
		if !ok {
			*errs = append(*errs, fmt.Sprintf("%s: expected an object like {label, value}, got %s", path, typeName(value)))
			return
		}
		if _, ok := obj["label"].(string); !ok {
			*errs = append(*errs, fmt.Sprintf("%s.label: expected string, got %s", path, typeName(obj["label"])))
		}
		if !isFiniteNumber(obj["value"]) {
			*errs = append(*errs, fmt.Sprintf("%s.value: expected a finite number, got %s", path, typeName(obj["value"])))
		}
	case kindRows:
		arr, ok := value.([]any)
		if !ok {
			*errs = append(*errs, fmt.Sprintf("%s: expected an array of row arrays, got %s", path, typeName(value)))
			return
		}
		for i, rowAny := range arr {
			row, ok := rowAny.([]any)
			if !ok {
				*errs = append(*errs, fmt.Sprintf("%s[%d]: expected an array of cells, got %s", path, i, typeName(rowAny)))
				continue
			}
			for j, cell := range row {
				if !isValidCell(cell) {
					*errs = append(*errs, fmt.Sprintf("%s[%d][%d]: expected string, number, boolean or null, got %s", path, i, j, typeName(cell)))
				}
			}
		}
	}
}

func isValidCell(v any) bool {
	if v == nil {
		return true
	}
	switch v.(type) {
	case string, bool:
		return true
	default:
		return isFiniteNumber(v)
	}
}

// isFiniteNumber reports whether v decodes to a finite number. encoding/json
// never produces NaN/Inf, but a caller constructing a Page programmatically
// (or a future non-JSON input path) could.
func isFiniteNumber(v any) bool {
	f, ok := toFloat(v)
	if !ok {
		return false
	}
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

func enumContains(values []any, v any) bool {
	for _, candidate := range values {
		if candidate == v {
			return true
		}
		// JSON numbers decode to float64; an enum of ints (heading level)
		// compares by value, not by concrete numeric type.
		if cf, ok := toFloat(candidate); ok {
			if vf, ok2 := toFloat(v); ok2 && cf == vf {
				return true
			}
		}
	}
	return false
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

// rawComponent is one components[i] entry as decoded from JSON, before
// type/shape validation.
func validateComponent(comp any, index int, errs *[]string) (id, ctype, parent string, fields map[string]any, ok bool) {
	path := fmt.Sprintf("components[%d]", index)
	obj, isObj := comp.(map[string]any)
	if !isObj {
		*errs = append(*errs, fmt.Sprintf("%s: expected an object, got %s", path, typeName(comp)))
		return "", "", "", nil, false
	}

	var idVal string
	if s, ok := obj["id"].(string); !ok || s == "" {
		*errs = append(*errs, fmt.Sprintf("%s.id: every component needs a non-empty string id", path))
	} else {
		idVal = s
	}

	if pv, present := obj["parent"]; present {
		if s, ok := pv.(string); ok {
			parent = s
		} else {
			*errs = append(*errs, fmt.Sprintf("%s.parent: expected string, got %s", path, typeName(pv)))
		}
	}

	typeVal, _ := obj["type"].(string)
	spec, known := Components[typeVal]
	if !known {
		*errs = append(*errs, fmt.Sprintf("%s.type: unknown type %s. Valid types: %s", path, jsonQuote(obj["type"]), strings.Join(componentTypeNames, ", ")))
		return idVal, typeVal, parent, nil, false
	}

	for field, fieldSpec := range spec.req {
		if v, present := obj[field]; !present {
			*errs = append(*errs, fmt.Sprintf("%s.%s: required for type %q", path, field, typeVal))
		} else {
			checkValue(fieldSpec, v, fmt.Sprintf("%s.%s", path, field), errs)
		}
	}
	for field, fieldSpec := range spec.opt {
		if v, present := obj[field]; present {
			checkValue(fieldSpec, v, fmt.Sprintf("%s.%s", path, field), errs)
		}
	}

	known2 := map[string]bool{"id": true, "type": true, "parent": true}
	for f := range spec.req {
		known2[f] = true
	}
	for f := range spec.opt {
		known2[f] = true
	}
	allowedNames := make([]string, 0, len(known2))
	for f := range known2 {
		allowedNames = append(allowedNames, f)
	}
	sort.Strings(allowedNames)
	for field := range obj {
		if !known2[field] {
			*errs = append(*errs, fmt.Sprintf("%s.%s: %q has no such field. Allowed: %s", path, field, typeVal, strings.Join(allowedNames, ", ")))
		}
	}

	fields = make(map[string]any, len(obj))
	for f := range spec.req {
		if v, present := obj[f]; present {
			fields[f] = v
		}
	}
	for f := range spec.opt {
		if v, present := obj[f]; present {
			fields[f] = v
		}
	}

	return idVal, typeVal, parent, fields, true
}

// Validate validates raw as an A2UI page. raw may be a JSON string, a []byte
// of JSON, or an already-decoded value (map[string]any/[]any/...) — models
// served through some providers/tool-calling shims hand back a parsed object
// for a "page" parameter typed as an object rather than re-stringifying it,
// so both are accepted rather than erroring on the more helpful shape.
//
// On failure the error's message is newline-joined and JSON-path-prefixed —
// the exact format schema.js's validatePage produces — since it is fed
// straight back to the model as the tool error for self-correction.
func Validate(raw any) (Page, error) {
	data, err := decode(raw)
	if err != nil {
		return Page{}, err
	}
	return validateDecoded(data)
}

func decode(raw any) (any, error) {
	switch v := raw.(type) {
	case string:
		var out any
		if err := json.Unmarshal([]byte(v), &out); err != nil {
			return nil, fmt.Errorf("invalid JSON: %v", err)
		}
		return out, nil
	case []byte:
		var out any
		if err := json.Unmarshal(v, &out); err != nil {
			return nil, fmt.Errorf("invalid JSON: %v", err)
		}
		return out, nil
	default:
		return raw, nil
	}
}

func validateDecoded(data any) (Page, error) {
	var errs []string

	obj, isObj := data.(map[string]any)
	if !isObj {
		return Page{}, fmt.Errorf("expected an object with \"title\" and \"components\", got %s", typeName(data))
	}

	var title string
	if s, ok := obj["title"].(string); !ok || s == "" {
		errs = append(errs, "title: expected a non-empty string")
	} else {
		title = s
	}

	compsRaw, hasComps := obj["components"]
	compsArr, isArr := compsRaw.([]any)
	if !hasComps || !isArr {
		errs = append(errs, fmt.Sprintf("components: expected an array, got %s", typeName(compsRaw)))
		return Page{}, fmt.Errorf("%s", strings.Join(errs, "\n"))
	}
	if len(compsArr) == 0 {
		errs = append(errs, "components: must contain at least one component")
	}
	if len(compsArr) > maxComponents {
		errs = append(errs, fmt.Sprintf("components: page has %d components, exceeding the maximum of %d", len(compsArr), maxComponents))
	}

	type parsed struct {
		id, ctype, parent string
		fields            map[string]any
		ok                bool
	}
	results := make([]parsed, len(compsArr))
	for i, c := range compsArr {
		id, ctype, parent, fields, ok := validateComponent(c, i, &errs)
		results[i] = parsed{id, ctype, parent, fields, ok}
	}

	// Structural checks the field validator cannot see: duplicate ids,
	// missing parents, self-parenting, and (beyond schema.js) parent cycles
	// — the renderer builds its tree by walking parent -> children, and an
	// undetected cycle there is an infinite loop, not just a bad page.
	ids := map[string]bool{}
	for _, r := range results {
		if r.id == "" {
			continue
		}
		if ids[r.id] {
			errs = append(errs, fmt.Sprintf("Duplicate component id: %q", r.id))
		}
		ids[r.id] = true
	}
	parentOf := map[string]string{}
	for _, r := range results {
		if r.id == "" || r.parent == "" {
			continue
		}
		if !ids[r.parent] {
			errs = append(errs, fmt.Sprintf("Component %q references non-existent parent %q", r.id, r.parent))
			continue
		}
		if r.parent == r.id {
			errs = append(errs, fmt.Sprintf("Component %q lists itself as its own parent", r.id))
			continue
		}
		parentOf[r.id] = r.parent
	}

	state := map[string]int{} // 0 unvisited, 1 visiting, 2 done
	var visit func(id string) bool
	visit = func(id string) bool {
		switch state[id] {
		case 1:
			return true
		case 2:
			return false
		}
		state[id] = 1
		if p, ok := parentOf[id]; ok {
			if visit(p) {
				return true
			}
		}
		state[id] = 2
		return false
	}
	reported := map[string]bool{}
	// Iterate ids in the order components were declared for deterministic
	// error output.
	for _, r := range results {
		if r.id == "" {
			continue
		}
		if _, hasParent := parentOf[r.id]; !hasParent {
			continue
		}
		if visit(r.id) && !reported[r.id] {
			errs = append(errs, fmt.Sprintf("Component %q is part of a parent cycle", r.id))
			reported[r.id] = true
		}
	}

	if len(errs) > 0 {
		return Page{}, fmt.Errorf("%s", strings.Join(errs, "\n"))
	}

	components := make([]Component, len(results))
	for i, r := range results {
		components[i] = Component{ID: r.id, Type: r.ctype, Parent: r.parent, Fields: r.fields}
	}
	return Page{Title: title, Components: components}, nil
}
