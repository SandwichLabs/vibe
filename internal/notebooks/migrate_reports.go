package notebooks

// MigrateReports converts every not-yet-migrated `_vibe_reports` row into a
// notebook (#68: the Access application layer's banded reports fold into
// notebooks). It is idempotent (a report whose migrated_to column is already
// set is skipped) and additive: report rows are never deleted or altered
// beyond stamping migrated_to, and a file with no `_vibe_reports` table at
// all (a new file created after #68, or one #68's migration already ran on)
// is a no-op rather than an error.
//
// Mapping: a report becomes a notebook with a markdown title cell, one sql
// cell over the report's bound source (projecting its detail columns, if
// any), and — when the report used banding/grouping/aggregates, structure a
// flat notebook cell cannot represent — a markdown note listing what was not
// migrated, with the original report spec kept below it as reference. PDF
// export (ExportReportPDF) is also dropped; the note says so, pointing at
// notebook print/PDF export as a future follow-up.
import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/sandwich-labs/vibe-app/internal/engine"
)

// reportGroupLevel / reportSortSpec / reportColumn / reportAggregate /
// reportBands mirror the JSON shapes the (now-removed) internal/reports
// package persisted to _vibe_reports, just enough to read an old row back
// out for migration.
type reportGroupLevel struct {
	Field   string `json:"field"`
	SortDir string `json:"sort_dir"`
}

type reportSortSpec struct {
	Field string `json:"field"`
	Dir   string `json:"dir"`
}

type reportColumn struct {
	Field   string `json:"field"`
	Caption string `json:"caption,omitempty"`
}

type reportAggregate struct {
	Name  string `json:"name"`
	Func  string `json:"func"`
	Field string `json:"field"`
}

type reportBands struct {
	Detail struct {
		Columns []reportColumn `json:"columns"`
	} `json:"detail"`
	GroupFooterAggregates  []reportAggregate `json:"group_footer_aggregates,omitempty"`
	ReportFooterAggregates []reportAggregate `json:"report_footer_aggregates,omitempty"`
}

// MigrateReports returns the number of reports migrated.
func MigrateReports(db *sql.DB, logger *slog.Logger) (int, error) {
	exists, err := tableExists(db, "_vibe_reports")
	if err != nil {
		return 0, fmt.Errorf("failed to check for _vibe_reports: %w", err)
	}
	if !exists {
		return 0, nil
	}

	if err := ensureReportsMigratedToColumn(db); err != nil {
		return 0, fmt.Errorf("failed to prepare _vibe_reports for migration: %w", err)
	}

	rows, err := db.Query(
		`SELECT id, title, source, groups, sort, bands FROM _vibe_reports
		  WHERE migrated_to IS NULL OR migrated_to = ''
		  ORDER BY title, id`,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to list reports to migrate: %w", err)
	}
	type reportRow struct{ id, title, source, groupsJSON, sortJSON, bandsJSON string }
	var toMigrate []reportRow
	for rows.Next() {
		var r reportRow
		if err := rows.Scan(&r.id, &r.title, &r.source, &r.groupsJSON, &r.sortJSON, &r.bandsJSON); err != nil {
			rows.Close() //nolint:errcheck
			return 0, fmt.Errorf("failed to scan report: %w", err)
		}
		toMigrate = append(toMigrate, r)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	rows.Close() //nolint:errcheck

	migrated := 0
	for _, r := range toMigrate {
		nb := migrateReport(r.id, r.title, r.source, r.groupsJSON, r.sortJSON, r.bandsJSON)
		if err := migrateOneReport(db, r.id, nb); err != nil {
			if logger != nil {
				logger.Warn("failed to migrate report, rolled back", "report_id", r.id, "error", err)
			}
			continue
		}
		migrated++
	}
	return migrated, nil
}

// tableExists reports whether a SQLite table of the given name exists.
func tableExists(db *sql.DB, table string) (bool, error) {
	var n int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table,
	).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// ensureReportsMigratedToColumn adds the migrated_to marker column to
// _vibe_reports if it is not already there. Only called once tableExists has
// confirmed the table is present, so this is safe on old files and a no-op
// (never even attempted) on a new file that never had the table.
func ensureReportsMigratedToColumn(db *sql.DB) error {
	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('_vibe_reports') WHERE name = 'migrated_to'`,
	).Scan(&n); err != nil {
		return fmt.Errorf("failed to inspect _vibe_reports.migrated_to: %w", err)
	}
	if n > 0 {
		return nil
	}
	if _, err := db.Exec(`ALTER TABLE _vibe_reports ADD COLUMN migrated_to TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("failed to add _vibe_reports.migrated_to: %w", err)
	}
	return nil
}

// migrateOneReport inserts nb and stamps the source report's migrated_to in
// ONE transaction, mirroring migrateOnePage's crash-safety rationale.
func migrateOneReport(db *sql.DB, reportID string, nb Notebook) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin migration transaction for report %q: %w", reportID, err)
	}
	defer tx.Rollback() //nolint:errcheck

	saved, _, err := Insert(tx, nb)
	if err != nil {
		return fmt.Errorf("failed to save migrated notebook for report %q: %w", reportID, err)
	}
	if _, err := tx.Exec(`UPDATE _vibe_reports SET migrated_to = ? WHERE id = ?`, saved.ID, reportID); err != nil {
		return fmt.Errorf("failed to mark report %q migrated: %w", reportID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit migration for report %q: %w", reportID, err)
	}
	return nil
}

// migrateReport converts a single report row into a Notebook (not yet
// persisted): a markdown title cell, one sql cell over the report's source
// (projecting its detail columns, if the report specified any), and — when
// the report used grouping/aggregates the flat cell model can't represent —
// a markdown note explaining what was dropped, with the original spec kept
// as reference.
func migrateReport(reportID, title, source, groupsJSON, sortJSON, bandsJSON string) Notebook {
	displayTitle := title
	if strings.TrimSpace(displayTitle) == "" {
		displayTitle = reportID
	}
	nb := Empty(displayTitle)

	var groups []reportGroupLevel
	_ = json.Unmarshal([]byte(groupsJSON), &groups) //nolint:errcheck
	var sorts []reportSortSpec
	_ = json.Unmarshal([]byte(sortJSON), &sorts) //nolint:errcheck
	var bands reportBands
	_ = json.Unmarshal([]byte(bandsJSON), &bands) //nolint:errcheck

	titleCell, _ := NewCell(CellMarkdown)
	titleCell.Source = fmt.Sprintf("# %s\n\nMigrated from a banded report (#68: reports fold into notebooks).", displayTitle)
	nb.Cells = append(nb.Cells, titleCell)

	sqlCell, _ := NewCell(CellSQL)
	sqlCell.Source = buildReportSelect(source, bands.Detail.Columns, sorts)
	sqlCell.View = "table"
	sqlCell.ViewPinned = true
	sqlCell.Label = displayTitle
	nb.Cells = append(nb.Cells, sqlCell)

	// The original-spec note cell is always added (review fix), not just
	// when banding/grouping/aggregates were present — even a "plain" report
	// (no grouping) had metadata (id, source, column order/captions) that is
	// useful to keep visible as a record of what was migrated from, and it
	// keeps the mapping predictable: every migrated report has exactly a
	// title cell, a sql cell, and a note cell, never a conditional third.
	noteCell, _ := NewCell(CellMarkdown)
	var parts []string
	if len(groups) > 0 {
		fields := make([]string, 0, len(groups))
		for _, g := range groups {
			fields = append(fields, g.Field)
		}
		parts = append(parts, fmt.Sprintf("%d group level(s) (%s)", len(groups), strings.Join(fields, ", ")))
	}
	if n := len(bands.GroupFooterAggregates) + len(bands.ReportFooterAggregates); n > 0 {
		parts = append(parts, fmt.Sprintf("%d aggregate(s)", n))
	}
	if len(parts) > 0 {
		noteCell.Source = "**Not migrated: banding/grouping.** The original report grouped and " +
			"summarized rows into bands (" + strings.Join(parts, ", ") + ") — a notebook cell is a flat " +
			"query result, so that structure could not be carried over. Re-derive it with `GROUP BY` " +
			"in a new sql cell, or keep the plain result above. PDF export was also dropped with the " +
			"report layer; notebook print/PDF export may come later. The original report spec is kept " +
			"below for reference.\n\n" + fence("json", reportSpecJSON(reportID, title, source, groups, sorts, bands))
	} else {
		noteCell.Source = "This report had no grouping or aggregates to lose — it migrated as a plain " +
			"query. PDF export was dropped with the report layer; notebook print/PDF export may come " +
			"later. The original report spec is kept below for reference.\n\n" +
			fence("json", reportSpecJSON(reportID, title, source, groups, sorts, bands))
	}
	nb.Cells = append(nb.Cells, noteCell)

	return nb
}

// buildReportSelect renders a SELECT over source, projecting the report's
// detail columns (aliased to their captions) when it declared any, or a
// bare `SELECT * FROM source` otherwise. source is either a plain table
// name (the common case — quoted as an identifier) or an arbitrary SQL
// query (a saved named-query SQL string, ReportSpec.Source's other historic
// use), wrapped as a subquery; every column/sort field is quoted via
// engine.QuoteIdent so a column named e.g. "Order Date" or a reserved word
// round-trips correctly instead of producing invalid or injectable SQL.
func buildReportSelect(source string, columns []reportColumn, sorts []reportSortSpec) string {
	trimmedSource := strings.TrimSpace(source)
	var from string
	if engine.ValidIdentifier(trimmedSource) {
		from = engine.QuoteIdent(trimmedSource)
	} else {
		// Not a plain identifier: treat as an arbitrary query and wrap it as
		// a subquery rather than splicing it in as a bare FROM target.
		from = "(" + source + ") AS report_source"
	}

	sel := "*"
	if len(columns) > 0 {
		parts := make([]string, 0, len(columns))
		for _, c := range columns {
			field := engine.QuoteIdent(c.Field)
			if c.Caption != "" && c.Caption != c.Field {
				parts = append(parts, fmt.Sprintf("%s AS %s", field, engine.QuoteIdent(c.Caption)))
			} else {
				parts = append(parts, field)
			}
		}
		sel = strings.Join(parts, ", ")
	}

	q := fmt.Sprintf("SELECT %s FROM %s", sel, from)
	if len(sorts) > 0 {
		parts := make([]string, 0, len(sorts))
		for _, s := range sorts {
			dir := "ASC"
			if strings.EqualFold(s.Dir, "desc") {
				dir = "DESC"
			}
			parts = append(parts, fmt.Sprintf("%s %s", engine.QuoteIdent(s.Field), dir))
		}
		q += " ORDER BY " + strings.Join(parts, ", ")
	}
	return q
}

// reportSpecJSON re-serializes the original report row as pretty JSON for
// the migration note's reference block.
func reportSpecJSON(id, title, source string, groups []reportGroupLevel, sorts []reportSortSpec, bands reportBands) string {
	spec := map[string]any{
		"id":     id,
		"title":  title,
		"source": source,
		"groups": groups,
		"sort":   sorts,
		"detail": bands.Detail,
	}
	if len(bands.GroupFooterAggregates) > 0 {
		spec["group_footer_aggregates"] = bands.GroupFooterAggregates
	}
	if len(bands.ReportFooterAggregates) > 0 {
		spec["report_footer_aggregates"] = bands.ReportFooterAggregates
	}
	b, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(b)
}
