package html

import (
	"context"
	"fmt"
	"html"
	"strings"

	"github.com/sandwich-labs/vibe-app/internal/engine"
	"github.com/sandwich-labs/vibe-app/internal/renderer"
)

// subformQueryRunner is the subset of the engine used to execute the
// auto-filter query for a master-detail subform. The production
// engineOptionResolver satisfies this so RenderSubform can reuse the same
// engine-backed resolver the {form} controls already use.
type subformQueryRunner interface {
	runQuery(ctx context.Context, sql string) (*engine.ResultSet, error)
}

// RenderSubform renders an Access-style master-detail subform: an FK-linked
// child datasheet auto-filtered to the current master record (plan §1.5).
//
// The auto-filter is expressed as a real WHERE <child_key> = <master key>
// clause executed against the engine — the whole child table is never loaded
// and filtered in Go.
func (r *Renderer) RenderSubform(ctx context.Context, cfg *renderer.SubformConfig, resolver OptionResolver) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("subform config is required")
	}
	if !isSubformIdentifier(cfg.ChildTable) {
		return "", fmt.Errorf("invalid subform child table %q", cfg.ChildTable)
	}
	if !isSubformIdentifier(cfg.ChildKey) {
		return "", fmt.Errorf("invalid subform child key %q", cfg.ChildKey)
	}

	runner, ok := resolver.(subformQueryRunner)
	if !ok {
		return "", fmt.Errorf("subform requires an engine-backed option resolver")
	}

	sql := fmt.Sprintf("SELECT * FROM %s WHERE %s = %s",
		cfg.ChildTable, cfg.ChildKey, sqlLiteral(cfg.MasterKey))

	rs, err := runner.runQuery(ctx, sql)
	if err != nil {
		return "", fmt.Errorf("failed to load subform child rows: %w", err)
	}

	// Map the engine result set into the renderer's datasheet shape.
	cols := make([]renderer.Column, 0, len(rs.Columns))
	for _, c := range rs.Columns {
		cols = append(cols, renderer.Column{Name: c.Name, Type: c.Type})
	}
	tblCfg := &renderer.TableConfig{
		Result: &renderer.ResultSet{Columns: cols, Rows: rs.Rows},
	}

	table, err := r.RenderTable(tblCfg)
	if err != nil {
		return "", fmt.Errorf("failed to render subform datasheet: %w", err)
	}

	var b strings.Builder
	b.WriteString(`<div class="vibe-subform">`)
	if strings.TrimSpace(cfg.Title) != "" {
		b.WriteString(fmt.Sprintf(`<div class="vibe-subform-title">%s</div>`, html.EscapeString(cfg.Title)))
	}
	b.WriteString(table)
	b.WriteString(`</div>`)
	return b.String(), nil
}

// sqlLiteral renders a master-key value as a SQL literal for the auto-filter
// WHERE clause. Numeric keys are emitted bare; everything else is single-quoted
// with quotes doubled.
func sqlLiteral(v any) string {
	switch n := v.(type) {
	case nil:
		return "NULL"
	case int:
		return fmt.Sprintf("%d", n)
	case int32:
		return fmt.Sprintf("%d", n)
	case int64:
		return fmt.Sprintf("%d", n)
	case float32:
		return fmt.Sprintf("%v", n)
	case float64:
		return fmt.Sprintf("%v", n)
	default:
		s := fmt.Sprintf("%v", v)
		return "'" + strings.ReplaceAll(s, "'", "''") + "'"
	}
}

// isSubformIdentifier reports whether s is a safe bare SQL identifier
// (letters, digits, underscore; not starting with a digit).
func isSubformIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		isLetter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'
		isDigit := c >= '0' && c <= '9'
		if i == 0 && !isLetter {
			return false
		}
		if !isLetter && !isDigit {
			return false
		}
	}
	return true
}
