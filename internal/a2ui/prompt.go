package a2ui

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// typeOrder is the presentation order for the <a2ui> system-prompt block:
// simple types first, then the data-bound chart/table types grouped at the
// end, matching schoolwork-workspace's hand-written a2uiPrompt. Components
// itself (the source of truth for field names/required-ness) is a map, so
// this fixed slice is only about reading order, never about which types or
// fields exist — those are pulled from Components so the prompt can't drift
// from the validator.
var typeOrder = []string{
	"heading", "text", "divider", "callout", "badge", "list", "code", "container",
	"statRow", "stat", "table", "barChart", "lineChart",
	"areaChart", "columnChart", "ringChart", "radarChart", "sankeyChart", "heatmap", "streamChart",
}

func init() {
	if len(typeOrder) != len(Components) {
		panic("a2ui: typeOrder is out of sync with Components")
	}
	for _, t := range typeOrder {
		if _, ok := Components[t]; !ok {
			panic("a2ui: typeOrder references unknown type " + t)
		}
	}
}

// isColorEnum reports whether spec is exactly the shared color enum — those
// fields are documented once, globally, rather than inline per type.
func isColorEnum(spec fieldSpec) bool {
	if spec.kind != kindEnum || len(spec.values) != len(ColorTokens) {
		return false
	}
	for i, v := range spec.values {
		if s, ok := v.(string); !ok || s != ColorTokens[i] {
			return false
		}
	}
	return true
}

// hintInner renders a short shape hint for a non-color field, or "" for
// plain string/number/boolean fields that need no elaboration.
func hintInner(spec fieldSpec) string {
	switch spec.kind {
	case kindEnum:
		if isColorEnum(spec) {
			return ""
		}
		vals := make([]string, len(spec.values))
		for i, v := range spec.values {
			vals[i] = fmt.Sprintf("%v", v)
		}
		return strings.Join(vals, "|")
	case kindArray:
		if spec.of != nil && spec.of.kind == kindPoint {
			return "[{label, value}]"
		}
		return "string[]"
	case kindRows:
		return "array of cell arrays"
	case kindMotion:
		return "{easing, speed, stagger, stiffness, damping, mass}"
	case kindRange:
		if spec.max == math.MaxFloat64 {
			return "number > 0"
		}
		return fmt.Sprintf("number %v-%v", spec.min, spec.max)
	default:
		return ""
	}
}

func sortedFieldNames(m map[string]fieldSpec) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// fieldList renders one type's field summary, e.g.
// "text(required), muted, color" — required fields first, then optional,
// both alphabetical for determinism.
func fieldList(spec componentSpec) string {
	var parts []string
	for _, name := range sortedFieldNames(spec.req) {
		if inner := hintInner(spec.req[name]); inner != "" {
			parts = append(parts, fmt.Sprintf("%s(required, %s)", name, inner))
		} else {
			parts = append(parts, name+"(required)")
		}
	}
	for _, name := range sortedFieldNames(spec.opt) {
		if inner := hintInner(spec.opt[name]); inner != "" {
			parts = append(parts, fmt.Sprintf("%s(%s)", name, inner))
		} else {
			parts = append(parts, name)
		}
	}
	if len(parts) == 0 {
		return "—"
	}
	return strings.Join(parts, ", ")
}

func quotedList(ss []string) string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = `"` + s + `"`
	}
	return strings.Join(out, ", ")
}

// PromptBlock renders the <a2ui> system-prompt block documenting the
// component schema to the model, for present_data. It is generated from
// Components/ColorTokens/DataBound — the same maps Validate checks against —
// so the type list, field names, required-ness and color enum in the prompt
// can never drift out of sync with what the validator actually accepts; only
// the surrounding prose is hand-written.
func PromptBlock() string {
	var b strings.Builder
	b.WriteString("<a2ui>\n")
	b.WriteString("present_data takes an A2UI page as a JSON object (a JSON string is also accepted):\n\n")
	b.WriteString(`  {"title": "...", "components": [ {"id": "c1", "type": "...", ...}, ... ]}` + "\n\n")
	b.WriteString("Components form a flat list; nesting is by reference — a child sets\n")
	b.WriteString("\"parent\" to its parent's id. Every component needs a unique \"id\".\n\n")
	b.WriteString("Types and their fields (optional unless marked required):\n")
	for _, t := range typeOrder {
		fmt.Fprintf(&b, "  %-12s %s\n", t, fieldList(Components[t]))
	}
	fmt.Fprintf(&b, "\ncolor must be one of: %s.\n\n", quotedList(ColorTokens))
	b.WriteString("Data binding: " + strings.Join(DataBound, ", ") + " accept either inline data or\n")
	b.WriteString("a \"sql\" field. \"sql\" bindings are READ-ONLY queries over this workspace's\n")
	b.WriteString("connections — no INSERT/UPDATE/DELETE/DDL/multiple statements in ANY sandbox mode\n")
	b.WriteString("(the query re-runs on every render, so a write would fire each time); files only\n")
	b.WriteString("by a literal absolute path inside data/, as for run_query. Prefer inline data in present_data: you\n")
	b.WriteString("already have the values and the reply is a one-off, so there is nothing for a\n")
	b.WriteString("live query to buy you.\n\n")
	b.WriteString("A data-bound component may also set \"connection\" (a connection name, as accepted by\n")
	b.WriteString("introspect_schema) to run its \"sql\" with that connection as the default for\n")
	b.WriteString("unqualified table names; omit it for the workspace default.\n\n")
	b.WriteString("SQL result shape: barChart and lineChart want a label column then a numeric\n")
	b.WriteString("column (SELECT school, AVG(score) ...). stat wants a single number in the first\n")
	b.WriteString("column of the first row. table renders whatever columns come back.\n")
	b.WriteString("The other charts take inline \"columns\" + \"rows\" in exactly the shape their\n")
	b.WriteString("sql returns:\n")
	b.WriteString("  areaChart, columnChart, radarChart: a label column, then one numeric column per\n")
	b.WriteString("    series (SELECT month, revenue, cost ...); columnChart mode stacked|grouped.\n")
	b.WriteString("  ringChart: label, value[, max] per ring (max defaults to the \"max\" field, else 100).\n")
	b.WriteString("  sankeyChart: source, target, weight per flow.\n")
	b.WriteString("  heatmap: row label, column label, value per cell (SELECT node, hour, load ...).\n")
	b.WriteString("  streamChart: label, value in time order; \"refresh\" (seconds) re-runs its sql\n")
	b.WriteString("    while visible and keeps the last \"window\" points; values above \"threshold\" flag.\n\n")
	b.WriteString("Every chart (" + strings.Join(Charts, ", ") + ") animates in; \"motion\" tunes it:\n")
	b.WriteString("easing one of " + quotedList(Easings) + " (\"custom\" uses stiffness/damping/mass),\n")
	b.WriteString("speed 0.25-4, stagger true|false. Omit motion for the default spring.\n\n")
	b.WriteString("Do not add a heading component that just repeats the page's own \"title\" — the\n")
	b.WriteString("title already renders above the components, so a duplicate heading is redundant\n")
	b.WriteString("and, in the narrow panel this renders in, competes with it for space.\n")
	b.WriteString("</a2ui>")
	return b.String()
}
