package schema

// Navigation / switchboard definitions (plan §4.2/§4.5). Nav items model an
// Access Switchboard: a hierarchical menu where each item belongs to a
// switchboard "page" (ParentID nesting for drill-down), carries a human label,
// an action kind, a target id, and a sort order. The App Shell and switchboard
// generator read this store to render the locked-down navigation.
//
import (
	"database/sql"
	"fmt"
)

// Nav action kinds. run_macro is stored but inert until P5.
const (
	NavOpenForm        = "open_form"
	NavOpenReport      = "open_report"
	NavOpenPage        = "open_page"
	NavOpenSwitchboard = "open_switchboard"
	NavRunMacro        = "run_macro"
)

// NavItem is a single switchboard menu entry.
type NavItem struct {
	ID         string `json:"id"`
	ParentID   string `json:"parent_id"` // empty for a root switchboard page item
	Label      string `json:"label"`
	ActionKind string `json:"action_kind"` // open_form | open_report | open_page | open_switchboard | run_macro
	TargetID   string `json:"target_id"`
	SortOrder  int    `json:"sort_order"`
}

// NavStore provides read/write access to switchboard nav items stored in the
// SQLCipher container's _vibe_nav table.
type NavStore struct {
	db *sql.DB
}

// NewNavStore constructs a nav store over the given SQLCipher container DB.
func NewNavStore(db *sql.DB) *NavStore {
	return &NavStore{db: db}
}

// navColumns is the canonical SELECT column list, in scan order.
const navColumns = `id, parent_id, label, action_kind, target_id, sort_order`

// scanNavItem scans a single row into a NavItem.
func scanNavItem(rows *sql.Rows) (NavItem, error) {
	var it NavItem
	if err := rows.Scan(
		&it.ID, &it.ParentID, &it.Label, &it.ActionKind, &it.TargetID, &it.SortOrder,
	); err != nil {
		return NavItem{}, fmt.Errorf("failed to scan nav item: %w", err)
	}
	return it, nil
}

// SetNav upserts a single nav item keyed by ID.
func (s *NavStore) SetNav(item NavItem) error {
	_, err := s.db.Exec(
		`INSERT INTO _vibe_nav (
			id, parent_id, label, action_kind, target_id, sort_order
		) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			parent_id   = excluded.parent_id,
			label       = excluded.label,
			action_kind = excluded.action_kind,
			target_id   = excluded.target_id,
			sort_order  = excluded.sort_order;`,
		item.ID, item.ParentID, item.Label, item.ActionKind, item.TargetID, item.SortOrder,
	)
	if err != nil {
		return fmt.Errorf("failed to upsert nav item %q: %w", item.ID, err)
	}
	return nil
}

// GetNav returns the direct children of the given switchboard page id, ordered
// by sort_order. Passing the empty string returns the root-level items.
func (s *NavStore) GetNav(parentID string) ([]NavItem, error) {
	rows, err := s.db.Query(
		`SELECT `+navColumns+` FROM _vibe_nav
		  WHERE parent_id = ?
		  ORDER BY sort_order, id;`,
		parentID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query nav children of %q: %w", parentID, err)
	}
	defer rows.Close() //nolint:errcheck

	var out []NavItem
	for rows.Next() {
		it, err := scanNavItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate nav children of %q: %w", parentID, err)
	}
	return out, nil
}

// ListNav returns every nav item across all switchboard pages, ordered by
// (parent_id, sort_order), so callers can reconstruct the full tree.
func (s *NavStore) ListNav() ([]NavItem, error) {
	rows, err := s.db.Query(
		`SELECT ` + navColumns + ` FROM _vibe_nav ORDER BY parent_id, sort_order, id;`,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list nav items: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var out []NavItem
	for rows.Next() {
		it, err := scanNavItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate nav items: %w", err)
	}
	return out, nil
}
