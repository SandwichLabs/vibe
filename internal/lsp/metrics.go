package lsp

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/SandwichLabs/assay/internal/semantics"
)

// MetricDoc is what completion and hover know about a metric: its name, title,
// description, dimensions and the id of its saved query. It carries no SQL: not
// the measure and not the saved query's text.
type MetricDoc struct {
	Name        string   `json:"name"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Dimensions  []string `json:"dimensions,omitempty"`
	Query       string   `json:"query,omitempty"`
}

// MetricLister is where the metrics come from: a semantics.Store.
type MetricLister interface {
	Metrics(ctx context.Context) ([]semantics.Metric, error)
}

// loadMetricDocs reads every readable metric; a file that fails validation is
// left out (the store lists it with an error).
func loadMetricDocs(ctx context.Context, src MetricLister) ([]MetricDoc, error) {
	ms, err := src.Metrics(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list metrics: %w", err)
	}
	docs := make([]MetricDoc, 0, len(ms))
	for _, m := range ms {
		if m.Error != "" || m.Name == semantics.TruncatedStubName {
			continue
		}
		docs = append(docs, MetricDoc{Name: m.Name, Title: m.Title, Description: m.Description,
			Dimensions: append([]string(nil), m.Dimensions...), Query: m.Query})
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].Name < docs[j].Name })
	return docs, nil
}

// Metric returns the metric called name (case-insensitive), or nil.
func (c *Catalog) Metric(name string) *MetricDoc {
	if c == nil {
		return nil
	}
	for i := range c.Metrics {
		if strings.EqualFold(c.Metrics[i].Name, name) {
			return &c.Metrics[i]
		}
	}
	return nil
}

// metricArg is a cursor inside a plain 'string' argument of a metric(...) call.
type metricArg struct {
	// dim is false for the metric's name (the first argument) and true for a
	// dimension of by =>.
	dim    bool
	metric string // the name literal of the call ("" while it is the one being typed)
	text   string // the whole literal under the cursor, unquoted
	from   int    // byte offset of the first character after the opening quote
	prefix string // the literal up to the cursor
}

// metricArgAt finds the metric( call whose string argument holds offset. For
// completion the cursor must be before the closing quote; for hover (inclusive)
// anywhere on the literal.
func metricArgAt(text string, offset int, inclusive bool) (metricArg, bool) {
	all := lex(text)
	s, e := statementBounds(all, offset, len(text))
	var toks []token
	for _, t := range all {
		if t.start >= s && t.end <= e && t.kind != tokComment {
			toks = append(toks, t)
		}
	}
	for i, t := range toks {
		if t.kind != tokString || t.start >= len(text) || text[t.start] != '\'' || offset <= t.start {
			continue
		}
		last := t.end - 1 // before the closing quote
		if t.open || inclusive {
			last = t.end
		}
		if offset > last {
			continue
		}
		p := openParenBefore(toks, i)
		if p < 1 || !toks[p-1].isWord("METRIC") {
			return metricArg{}, false
		}
		arg := metricArg{dim: i != p+1, text: t.text, from: t.start + 1, prefix: text[t.start+1 : min(offset, t.end)]}
		if i != p+1 {
			if toks[p+1].kind != tokString {
				return metricArg{}, false
			}
			arg.metric = toks[p+1].text
		}
		return arg, true
	}
	return metricArg{}, false
}

func (t token) isWord(upper string) bool { return t.kind == tokWord && t.upper() == upper }

// openParenBefore returns the index of the "(" that encloses toks[i], or -1.
func openParenBefore(toks []token, i int) int {
	depth := 0
	for j := i - 1; j >= 0; j-- {
		if toks[j].kind != tokPunct {
			continue
		}
		switch toks[j].text {
		case ")":
			depth++
		case "(":
			if depth == 0 {
				return j
			}
			depth--
		}
	}
	return -1
}

// metricItems completes inside metric('<here>' and by => '<here>': the
// metrics' names, then the named metric's dimensions. ok is false when the
// cursor is not in such a literal (the caller then completes as usual).
func metricItems(text string, offset int, cat *Catalog) (items []Item, from int, ok bool) {
	arg, ok := metricArgAt(text, offset, false)
	if !ok {
		return nil, 0, false
	}
	lp := strings.ToLower(arg.prefix)
	items = []Item{}
	if !arg.dim {
		if cat != nil {
			for _, m := range cat.Metrics {
				if strings.HasPrefix(strings.ToLower(m.Name), lp) {
					items = append(items, Item{Label: m.Name, Kind: KindMetric, Detail: metricDetail(m)})
				}
			}
		}
		return items, arg.from, true
	}
	if m := cat.Metric(arg.metric); m != nil {
		for _, d := range m.Dimensions {
			if strings.HasPrefix(strings.ToLower(d), lp) {
				items = append(items, Item{Label: d, Kind: KindColumn, Detail: "dimension of " + m.Name})
			}
		}
	}
	return items, arg.from, true
}

func metricDetail(m MetricDoc) string {
	d := m.Title
	if d == "" {
		d = "metric"
	}
	if len(m.Dimensions) > 0 {
		d += " by " + strings.Join(m.Dimensions, ", ")
	}
	return d
}

// metricHover describes the metric (or dimension) literal under the cursor.
func metricHover(text string, offset int, cat *Catalog) string {
	arg, ok := metricArgAt(text, offset, true)
	if !ok {
		return ""
	}
	if arg.dim {
		if m := cat.Metric(arg.metric); m != nil {
			for _, d := range m.Dimensions {
				if strings.EqualFold(d, arg.text) {
					return fmt.Sprintf("**dimension** `%s` of metric `%s`", d, m.Name)
				}
			}
		}
		return ""
	}
	m := cat.Metric(arg.text)
	if m == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "**metric** `%s`", m.Name)
	if m.Title != "" {
		fmt.Fprintf(&b, " — %s", m.Title)
	}
	b.WriteString("\n\n")
	if m.Description != "" {
		b.WriteString(m.Description + "\n\n")
	}
	if len(m.Dimensions) > 0 {
		fmt.Fprintf(&b, "- dimensions: %s\n", strings.Join(m.Dimensions, ", "))
	}
	if m.Query != "" {
		fmt.Fprintf(&b, "- saved query: `%s`\n", m.Query)
	}
	return b.String()
}

// metricFunctionItem is how `metric` itself is offered where a table or an
// expression may start.
func metricFunctionItem(cat *Catalog) []Item {
	if cat == nil || len(cat.Metrics) == 0 {
		return nil
	}
	return []Item{{Label: "metric", Kind: KindFunction, Detail: "metric('name', by => 'dimension')"}}
}
