package workspace

import (
	"context"
	"errors"
	"sort"

	"github.com/SandwichLabs/slaw/internal/artifacts"
	"github.com/SandwichLabs/slaw/internal/queries"
	"github.com/SandwichLabs/slaw/internal/semantics"
)

// A script view or HTML page whose queries call metric() reads files its own
// hash does not cover: the metric's yaml and the saved query behind it. A git
// pull that changes either changes what an allowed page reads (and what an
// `ask` page sends to the model) without the person being asked (#282). So a
// page that calls metric() is trusted by artifacts.DepsHash over its own hash
// and the revisions of those files; a page that does not keeps its own hash
// and every decision made on it.

// ArtifactMetricUse is a metric an artifact's queries call, for the allow
// prompt and Settings > Workspace > Pages.
type ArtifactMetricUse struct {
	Name string `json:"name"`
	// Query is the saved query the metric reads ("" when the metric file could
	// not be read).
	Query string `json:"query,omitempty"`
	// Measure and Dimensions are what the metric file says now, so the prompt can
	// show what changed ("" and none when the file cannot be read).
	Measure    string   `json:"measure,omitempty"`
	Dimensions []string `json:"dimensions,omitempty"`
	// Missing is true when no metric file by that name exists (the call fails
	// when run, until one does; the day it appears the page asks again).
	Missing bool `json:"missing,omitempty"`
}

const (
	depMetric = semantics.PinMetric
	depQuery  = semantics.PinQuery
	// depMissing and depUnreadable stand for the revision of a file that is not
	// there or cannot be read, so either way the hash is stable and changes
	// when the file appears or is fixed.
	depMissing    = semantics.PinMissing
	depUnreadable = "unreadable"
)

// applyMetricDeps notes the metrics the given query texts call and, when there
// are any, folds their files into st.Hash. st.ContentHash always holds the
// page's own hash (what a host re-derives from the bytes it holds).
func (w *workspace) applyMetricDeps(ctx context.Context, st *ArtifactViewStatus, sqls []string) {
	st.ContentHash = st.Hash
	// Always set (empty for a view that reads no metric): pins are strict once attached.
	st.Pins = map[string]string{}
	seen := map[string]bool{}
	var names []string
	for _, sql := range sqls {
		for _, n := range semantics.CalledMetrics(sql) {
			if !seen[n] {
				seen[n] = true
				names = append(names, n)
			}
		}
	}
	if len(names) == 0 {
		return
	}
	var parts []artifacts.DepPart
	sort.Strings(names)
	for _, n := range names {
		use, ps := w.metricDep(ctx, n)
		st.MetricsUsed = append(st.MetricsUsed, use)
		parts = append(parts, ps...)
	}
	st.Hash = artifacts.DepsHash(st.ContentHash, parts)
	st.Deps = parts
	for _, p := range parts {
		st.Pins[semantics.PinKey(p.Kind, p.Name)] = p.Rev
	}
	st.MetricsNote = artifacts.MetricsLine
}

// metricDep reads one metric's file and the saved query it names, as hashed.
func (w *workspace) metricDep(ctx context.Context, name string) (ArtifactMetricUse, []artifacts.DepPart) {
	use := ArtifactMetricUse{Name: name}
	src, err := w.Semantics().Source(ctx, semantics.KindMetric, name)
	switch {
	case errors.Is(err, semantics.ErrNotFound):
		use.Missing = true
		return use, []artifacts.DepPart{{Kind: depMetric, Name: name, Rev: depMissing}}
	case err != nil:
		return use, []artifacts.DepPart{{Kind: depMetric, Name: name, Rev: depUnreadable}}
	}
	parts := []artifacts.DepPart{{Kind: depMetric, Name: name, Rev: src.Rev}}
	m, err := w.Semantics().Metric(ctx, name)
	if err != nil {
		return use, parts
	}
	use.Query, use.Measure, use.Dimensions = m.Query, m.Measure, m.Dimensions
	q, err := w.Queries().Get(ctx, m.Query)
	switch {
	case errors.Is(err, queries.ErrNotFound):
		parts = append(parts, artifacts.DepPart{Kind: depQuery, Name: m.Query, Rev: depMissing})
	case err != nil || q == nil:
		parts = append(parts, artifacts.DepPart{Kind: depQuery, Name: m.Query, Rev: depUnreadable})
	default:
		parts = append(parts, artifacts.DepPart{Kind: depQuery, Name: m.Query, Rev: semantics.QueryRevision(q)})
	}
	return use, parts
}
