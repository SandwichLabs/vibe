package schema

// Form definitions (plan §1.5/§1.6). Forms are stored in the SQLCipher
// container's _vibe_forms table as versioned JSON. A form binds a table to a
// set of bound controls and an archetype (columnar | continuous | datasheet |
// master_detail); a master_detail form additionally carries a Subform link.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// FormSpec is a stored form definition.
type FormSpec struct {
	ID        string          `json:"id"`
	Title     string          `json:"title"`
	Table     string          `json:"table"`
	Archetype string          `json:"archetype"` // columnar | continuous | datasheet | master_detail
	OnSubmit  string          `json:"on_submit"` // refresh | redirect:<page> | noop
	Fields    []FormFieldSpec `json:"fields"`
	Subform   *SubformSpec    `json:"subform,omitempty"`
}

// FormFieldSpec is a single bound control on a form or subform.
type FormFieldSpec struct {
	Name        string `json:"name"`
	Caption     string `json:"caption"`
	ControlType string `json:"control_type"`
}

// SubformSpec is the master-detail link for an embedded child datasheet.
// child.<ChildKey> = master.<ParentKey>.
type SubformSpec struct {
	ChildTable string          `json:"child_table"`
	ChildKey   string          `json:"child_key"`
	ParentKey  string          `json:"parent_key"`
	Fields     []FormFieldSpec `json:"fields"`
}

// FormSummary is a lightweight listing entry for _vibe_forms.
type FormSummary struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Archetype string `json:"archetype"`
}

// FormStore provides read/write access to form definitions stored as versioned
// JSON in the SQLCipher container's _vibe_forms table.
type FormStore struct {
	db *sql.DB
}

// NewFormStore constructs a form store over the given SQLCipher container DB.
func NewFormStore(db *sql.DB) *FormStore {
	return &FormStore{db: db}
}

// SaveForm upserts a form definition, persisting the full spec as versioned
// JSON. The version counter is bumped on every save.
func (s *FormStore) SaveForm(spec FormSpec) error {
	if spec.ID == "" {
		return fmt.Errorf("form id is required")
	}
	blob, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("failed to marshal form spec %q: %w", spec.ID, err)
	}
	now := time.Now().UTC().Format(time.RFC3339)

	_, err = s.db.Exec(
		`INSERT INTO _vibe_forms (id, title, table_name, archetype, version, spec, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 1, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
			title      = excluded.title,
			table_name = excluded.table_name,
			archetype  = excluded.archetype,
			version    = _vibe_forms.version + 1,
			spec       = excluded.spec,
			updated_at = excluded.updated_at;`,
		spec.ID, spec.Title, spec.Table, spec.Archetype, string(blob), now, now,
	)
	if err != nil {
		return fmt.Errorf("failed to upsert form %q: %w", spec.ID, err)
	}
	return nil
}

// GetForm reads a stored form definition by id, decoding the versioned JSON spec.
func (s *FormStore) GetForm(id string) (*FormSpec, error) {
	var blob string
	err := s.db.QueryRow(`SELECT spec FROM _vibe_forms WHERE id = ?;`, id).Scan(&blob)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("form %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query form %q: %w", id, err)
	}
	var spec FormSpec
	if err := json.Unmarshal([]byte(blob), &spec); err != nil {
		return nil, fmt.Errorf("failed to decode form spec %q: %w", id, err)
	}
	return &spec, nil
}

// ListForms returns a summary of every stored form, ordered by id.
func (s *FormStore) ListForms() ([]FormSummary, error) {
	rows, err := s.db.Query(`SELECT id, title, archetype FROM _vibe_forms ORDER BY id;`)
	if err != nil {
		return nil, fmt.Errorf("failed to list forms: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var out []FormSummary
	for rows.Next() {
		var fs FormSummary
		if err := rows.Scan(&fs.ID, &fs.Title, &fs.Archetype); err != nil {
			return nil, fmt.Errorf("failed to scan form summary: %w", err)
		}
		out = append(out, fs)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate forms: %w", err)
	}
	return out, nil
}
