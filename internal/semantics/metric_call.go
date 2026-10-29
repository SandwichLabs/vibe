package semantics

import (
	"sort"
	"strings"
)

// MetricCall writes the metric('name', by => [...]) call for a metric and a
// subset of its dimensions, the text a person's or a runner's statement embeds
// (a check, "Try it"). The names are quoted as plain literals with any quote
// doubled; the expander still refuses a name the store does not know and a
// dimension the metric does not declare, so the text can carry nothing else.
func MetricCall(name string, dims []string) string {
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	switch len(dims) {
	case 0:
		return "metric(" + q(name) + ")"
	case 1:
		return "metric(" + q(name) + ", by => " + q(dims[0]) + ")"
	}
	parts := make([]string, len(dims))
	for i, d := range dims {
		parts[i] = q(d)
	}
	return "metric(" + q(name) + ", by => [" + strings.Join(parts, ", ") + "])"
}

// CalledMetrics returns the distinct metric names text calls with a plain
// 'quoted' literal, sorted. It only lexes: it reads no store, so a metric that
// is missing is still listed. Text the expander would refuse for its grammar
// (a name that is not a plain literal) lists nothing for that call, since it
// cannot run.
func CalledMetrics(text string) []string {
	if !HasMetricCall(text) {
		return nil
	}
	calls, err := findCalls(text)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, c := range calls {
		if !seen[c.name] {
			seen[c.name] = true
			out = append(out, c.name)
		}
	}
	sort.Strings(out)
	return out
}
