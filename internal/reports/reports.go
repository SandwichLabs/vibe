package reports

// Banded report model (plan §3.2 / P3.7).
//
// Implements the Access-style banded report: a versioned JSON report spec
// persisted in the SQLCipher container's _vibe_reports table, plus a pure-Go
// band-computation step that groups + sorts + aggregates a DuckDB ResultSet
// into an ordered band tree (report header > group header > detail > group
// footer > report footer).

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/sandwich-labs/vibe-app/internal/engine"
)

// AggFunc identifies a banded-report aggregate function (=Sum/Count/Avg/Min/Max
// over a group or the whole report).
type AggFunc string

const (
	AggSum   AggFunc = "sum"
	AggCount AggFunc = "count"
	AggAvg   AggFunc = "avg"
	AggMin   AggFunc = "min"
	AggMax   AggFunc = "max"
)

// Aggregate is a named aggregate expression evaluated over a band's rows.
type Aggregate struct {
	Name  string  `json:"name"`
	Func  AggFunc `json:"func"`
	Field string  `json:"field"`
}

// GroupLevel is one level of grouping (Access group-header/footer band).
type GroupLevel struct {
	Field   string `json:"field"`
	SortDir string `json:"sort_dir"` // asc | desc
}

// SortSpec orders detail rows within a group.
type SortSpec struct {
	Field string `json:"field"`
	Dir   string `json:"dir"` // asc | desc
}

// ReportColumn is a detail-band column binding.
type ReportColumn struct {
	Field   string `json:"field"`
	Caption string `json:"caption,omitempty"`
}

// DetailBand describes the repeating per-row detail band.
type DetailBand struct {
	Columns []ReportColumn `json:"columns"`
}

// ChartSpec describes how the Report View charts this report's data: which
// column labels the x-axis, which (optional) column splits rows into stacked
// series, and which column supplies the numeric value. Absent fields are
// derived heuristically by the viewer (parity W6 / mockup #11).
type ChartSpec struct {
	LabelField  string `json:"label_field"`
	SeriesField string `json:"series_field,omitempty"`
	ValueField  string `json:"value_field"`
}

// ReportSpec is the stored, versioned banded-report definition.
type ReportSpec struct {
	ID                     string       `json:"id"`
	Title                  string       `json:"title"`
	Source                 string       `json:"source"`
	Groups                 []GroupLevel `json:"groups,omitempty"`
	Sort                   []SortSpec   `json:"sort,omitempty"`
	Detail                 DetailBand   `json:"detail"`
	GroupFooterAggregates  []Aggregate  `json:"group_footer_aggregates,omitempty"`
	ReportFooterAggregates []Aggregate  `json:"report_footer_aggregates,omitempty"`
	Chart                  *ChartSpec   `json:"chart,omitempty"`
}

// bands is the JSON payload persisted in the _vibe_reports.bands column: the
// detail band plus the group-footer / report-footer aggregate expressions.
// Chart rides along here so adding it needed no schema migration — old rows
// simply unmarshal with a nil Chart.
type bands struct {
	Detail                 DetailBand  `json:"detail"`
	GroupFooterAggregates  []Aggregate `json:"group_footer_aggregates,omitempty"`
	ReportFooterAggregates []Aggregate `json:"report_footer_aggregates,omitempty"`
	Chart                  *ChartSpec  `json:"chart,omitempty"`
}

// GroupBand is a computed group band: its key, nested detail rows, and computed
// group-footer aggregates.
type GroupBand struct {
	Key              string
	Details          [][]any
	FooterAggregates map[string]float64
}

// BandTree is the computed, ordered band tree for a report.
type BandTree struct {
	Groups           []GroupBand
	FooterAggregates map[string]float64
}

// ReportStore provides CRUD over report definitions stored as versioned JSON in
// the SQLCipher container's _vibe_reports table.
type ReportStore struct {
	db *sql.DB
}

// NewReportStore constructs a report store over the given SQLCipher container DB.
func NewReportStore(db *sql.DB) *ReportStore {
	return &ReportStore{db: db}
}

// Save upserts a report definition, marshalling the grouping/sort/band portions
// into their dedicated JSON columns.
func (s *ReportStore) Save(spec ReportSpec) error {
	groupsJSON, err := json.Marshal(spec.Groups)
	if err != nil {
		return fmt.Errorf("failed to marshal report groups: %w", err)
	}
	sortJSON, err := json.Marshal(spec.Sort)
	if err != nil {
		return fmt.Errorf("failed to marshal report sort: %w", err)
	}
	bandsJSON, err := json.Marshal(bands{
		Detail:                 spec.Detail,
		GroupFooterAggregates:  spec.GroupFooterAggregates,
		ReportFooterAggregates: spec.ReportFooterAggregates,
		Chart:                  spec.Chart,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal report bands: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.Exec(
		`INSERT INTO _vibe_reports (id, title, source, groups, sort, bands, version, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   title=excluded.title,
		   source=excluded.source,
		   groups=excluded.groups,
		   sort=excluded.sort,
		   bands=excluded.bands,
		   version=_vibe_reports.version+1,
		   updated_at=excluded.updated_at;`,
		spec.ID, spec.Title, spec.Source,
		string(groupsJSON), string(sortJSON), string(bandsJSON), now, now,
	)
	if err != nil {
		return fmt.Errorf("failed to save report %q: %w", spec.ID, err)
	}
	return nil
}

// Get reads a stored report definition by id, returning nil if not found.
func (s *ReportStore) Get(id string) (*ReportSpec, error) {
	var groupsJSON, sortJSON, bandsJSON string
	spec := ReportSpec{ID: id}
	err := s.db.QueryRow(
		`SELECT title, source, groups, sort, bands FROM _vibe_reports WHERE id = ?;`,
		id,
	).Scan(&spec.Title, &spec.Source, &groupsJSON, &sortJSON, &bandsJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read report %q: %w", id, err)
	}

	if err := json.Unmarshal([]byte(groupsJSON), &spec.Groups); err != nil {
		return nil, fmt.Errorf("failed to unmarshal report groups for %q: %w", id, err)
	}
	if err := json.Unmarshal([]byte(sortJSON), &spec.Sort); err != nil {
		return nil, fmt.Errorf("failed to unmarshal report sort for %q: %w", id, err)
	}
	var b bands
	if err := json.Unmarshal([]byte(bandsJSON), &b); err != nil {
		return nil, fmt.Errorf("failed to unmarshal report bands for %q: %w", id, err)
	}
	spec.Detail = b.Detail
	spec.GroupFooterAggregates = b.GroupFooterAggregates
	spec.ReportFooterAggregates = b.ReportFooterAggregates
	spec.Chart = b.Chart

	return &spec, nil
}

// ReportSummary is a lightweight listing entry (id + title + source) for the
// report picker, avoiding a full spec deserialize per row.
type ReportSummary struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Source string `json:"source"`
}

// List returns every stored report definition as a lightweight summary, ordered
// by title.
func (s *ReportStore) List() ([]ReportSummary, error) {
	rows, err := s.db.Query(`SELECT id, title, source FROM _vibe_reports ORDER BY title;`)
	if err != nil {
		return nil, fmt.Errorf("failed to list reports: %w", err)
	}
	defer rows.Close()

	var out []ReportSummary
	for rows.Next() {
		var r ReportSummary
		if err := rows.Scan(&r.ID, &r.Title, &r.Source); err != nil {
			return nil, fmt.Errorf("failed to scan report summary: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate reports: %w", err)
	}
	return out, nil
}

// ComputeBands groups + sorts + aggregates a query ResultSet into an ordered
// band tree. It currently supports a single top-level grouping level (the first
// entry in spec.Groups), which matches the Access "sales by rep" archetype.
func ComputeBands(rs *engine.ResultSet, spec ReportSpec) (*BandTree, error) {
	tree := &BandTree{
		Groups:           []GroupBand{},
		FooterAggregates: map[string]float64{},
	}
	if rs == nil {
		return tree, nil
	}

	// Group rows by the first grouping level's field (if any).
	var groupIdx = -1
	var groupDir = "asc"
	if len(spec.Groups) > 0 {
		groupIdx = rs.ColumnIndex(spec.Groups[0].Field)
		if groupIdx < 0 {
			return nil, fmt.Errorf("group field %q not found in result set", spec.Groups[0].Field)
		}
		if spec.Groups[0].SortDir == "desc" {
			groupDir = "desc"
		}
	}

	type bucket struct {
		key  string
		rows [][]any
	}
	var order []string
	buckets := map[string]*bucket{}
	for _, row := range rs.Rows {
		key := ""
		if groupIdx >= 0 {
			key = fmt.Sprintf("%v", row[groupIdx])
		}
		b, ok := buckets[key]
		if !ok {
			b = &bucket{key: key}
			buckets[key] = b
			order = append(order, key)
		}
		b.rows = append(b.rows, row)
	}

	// Order groups by key according to the group sort direction.
	sort.SliceStable(order, func(i, j int) bool {
		if groupDir == "desc" {
			return order[i] > order[j]
		}
		return order[i] < order[j]
	})

	// Detail sort comparator (first sort spec) applied within each group.
	var sortIdx = -1
	var sortDesc bool
	if len(spec.Sort) > 0 {
		sortIdx = rs.ColumnIndex(spec.Sort[0].Field)
		sortDesc = spec.Sort[0].Dir == "desc"
	}

	for _, key := range order {
		b := buckets[key]
		details := b.rows
		if sortIdx >= 0 {
			sort.SliceStable(details, func(i, j int) bool {
				less := compareValues(details[i][sortIdx], details[j][sortIdx]) < 0
				if sortDesc {
					return !less && compareValues(details[i][sortIdx], details[j][sortIdx]) != 0
				}
				return less
			})
		}
		gb := GroupBand{
			Key:              key,
			Details:          details,
			FooterAggregates: computeAggregates(rs, details, spec.GroupFooterAggregates),
		}
		tree.Groups = append(tree.Groups, gb)
	}

	tree.FooterAggregates = computeAggregates(rs, rs.Rows, spec.ReportFooterAggregates)
	return tree, nil
}

// computeAggregates evaluates each aggregate expression over the given rows.
func computeAggregates(rs *engine.ResultSet, rows [][]any, aggs []Aggregate) map[string]float64 {
	out := map[string]float64{}
	for _, a := range aggs {
		idx := rs.ColumnIndex(a.Field)
		var (
			sum   float64
			count int
			min   float64
			max   float64
			first = true
		)
		for _, row := range rows {
			if idx < 0 || idx >= len(row) {
				continue
			}
			v, ok := toFloat(row[idx])
			if a.Func != AggCount && !ok {
				continue
			}
			count++
			sum += v
			if first {
				min, max = v, v
				first = false
			} else {
				if v < min {
					min = v
				}
				if v > max {
					max = v
				}
			}
		}
		switch a.Func {
		case AggSum:
			out[a.Name] = sum
		case AggCount:
			out[a.Name] = float64(count)
		case AggAvg:
			if count > 0 {
				out[a.Name] = sum / float64(count)
			}
		case AggMin:
			if count > 0 {
				out[a.Name] = min
			}
		case AggMax:
			if count > 0 {
				out[a.Name] = max
			}
		default:
			out[a.Name] = sum
		}
	}
	return out
}

// compareValues orders two values numerically when possible, else lexically.
func compareValues(a, b any) int {
	af, aok := toFloat(a)
	bf, bok := toFloat(b)
	if aok && bok {
		switch {
		case af < bf:
			return -1
		case af > bf:
			return 1
		default:
			return 0
		}
	}
	as, bs := fmt.Sprintf("%v", a), fmt.Sprintf("%v", b)
	switch {
	case as < bs:
		return -1
	case as > bs:
		return 1
	default:
		return 0
	}
}

// toFloat coerces a scanned DuckDB value to float64.
func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}
