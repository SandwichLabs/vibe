package semantics

import "strings"

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
