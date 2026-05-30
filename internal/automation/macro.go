package automation

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Macro action kinds (plan §5.1). These are the declarative action engine's
// vocabulary; later units add execution.
const (
	ActionOpenForm    = "OpenForm"
	ActionOpenReport  = "OpenReport"
	ActionGoToRecord  = "GoToRecord"
	ActionFindRecord  = "FindRecord"
	ActionRunQuery    = "RunQuery"
	ActionRunSQL      = "RunSQL"
	ActionSetValue    = "SetValue"
	ActionSetFilter   = "SetFilter"
	ActionMsgBox      = "MsgBox"
	ActionExport      = "Export"
	ActionEmailObject = "EmailObject"
	ActionRunMacro    = "RunMacro"
)

// macroSpecVersion is the current version stamped onto stored macro specs so
// future migrations can detect older payloads.
const macroSpecVersion = 1

// knownActionKinds is the set of valid §5.1 action kinds, used by Validate.
var knownActionKinds = map[string]struct{}{
	ActionOpenForm:    {},
	ActionOpenReport:  {},
	ActionGoToRecord:  {},
	ActionFindRecord:  {},
	ActionRunQuery:    {},
	ActionRunSQL:      {},
	ActionSetValue:    {},
	ActionSetFilter:   {},
	ActionMsgBox:      {},
	ActionExport:      {},
	ActionEmailObject: {},
	ActionRunMacro:    {},
}

// ErrMacroNotFound is returned by MacroStore.Get when no macro has the given id.
var ErrMacroNotFound = errors.New("automation: macro not found")

// MacroCondition is an optional If/Then guard on a single action: the action
// runs only when the condition expression evaluates truthy.
type MacroCondition struct {
	Expr string `json:"expr"`
}

// MacroAction is one ordered step in a macro: an action kind plus a free-form
// args map and an optional If/Then condition.
type MacroAction struct {
	Kind      string            `json:"kind"`
	Args      map[string]string `json:"args,omitempty"`
	Condition *MacroCondition   `json:"condition,omitempty"`
}

// MacroSpec is the versioned, declarative definition of a macro: an ordered list
// of actions persisted as versioned JSON in the SQLCipher container.
type MacroSpec struct {
	ID      string        `json:"id"`
	Title   string        `json:"title"`
	Version int           `json:"version"`
	Actions []MacroAction `json:"actions"`
}

// Validate rejects a spec with no id or an unknown action kind.
func (s MacroSpec) Validate() error {
	if s.ID == "" {
		return fmt.Errorf("automation: macro id is required")
	}
	for i, a := range s.Actions {
		if _, ok := knownActionKinds[a.Kind]; !ok {
			return fmt.Errorf("automation: action %d has unknown kind %q", i, a.Kind)
		}
	}
	return nil
}

// MacroStore provides read/write access to macro definitions stored as versioned
// JSON in the SQLCipher container's _vibe_macros table, modeled on FormStore.
type MacroStore struct {
	db *sql.DB
}

// NewMacroStore constructs a macro store over the given SQLCipher container DB.
func NewMacroStore(db *sql.DB) *MacroStore {
	return &MacroStore{db: db}
}

// Save upserts a macro definition, persisting the full spec as versioned JSON.
func (s *MacroStore) Save(spec MacroSpec) error {
	if spec.ID == "" {
		return fmt.Errorf("automation: macro id is required")
	}
	spec.Version = macroSpecVersion
	blob, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("automation: failed to marshal macro spec %q: %w", spec.ID, err)
	}
	now := time.Now().UTC().Format(time.RFC3339)

	_, err = s.db.Exec(
		`INSERT INTO _vibe_macros (id, title, version, spec, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
			title      = excluded.title,
			version    = excluded.version,
			spec       = excluded.spec,
			updated_at = excluded.updated_at;`,
		spec.ID, spec.Title, spec.Version, string(blob), now, now,
	)
	if err != nil {
		return fmt.Errorf("automation: failed to upsert macro %q: %w", spec.ID, err)
	}
	return nil
}

// Get reads a macro by id, returning ErrMacroNotFound when absent.
func (s *MacroStore) Get(id string) (*MacroSpec, error) {
	var blob string
	err := s.db.QueryRow(`SELECT spec FROM _vibe_macros WHERE id = ?;`, id).Scan(&blob)
	if err == sql.ErrNoRows {
		return nil, ErrMacroNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("automation: failed to query macro %q: %w", id, err)
	}
	var spec MacroSpec
	if err := json.Unmarshal([]byte(blob), &spec); err != nil {
		return nil, fmt.Errorf("automation: failed to decode macro spec %q: %w", id, err)
	}
	return &spec, nil
}

// List returns every stored macro, ordered by id.
func (s *MacroStore) List() ([]MacroSpec, error) {
	rows, err := s.db.Query(`SELECT spec FROM _vibe_macros ORDER BY id;`)
	if err != nil {
		return nil, fmt.Errorf("automation: failed to list macros: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var out []MacroSpec
	for rows.Next() {
		var blob string
		if err := rows.Scan(&blob); err != nil {
			return nil, fmt.Errorf("automation: failed to scan macro: %w", err)
		}
		var spec MacroSpec
		if err := json.Unmarshal([]byte(blob), &spec); err != nil {
			return nil, fmt.Errorf("automation: failed to decode macro spec: %w", err)
		}
		out = append(out, spec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("automation: failed to iterate macros: %w", err)
	}
	return out, nil
}

// Delete removes a macro by id.
func (s *MacroStore) Delete(id string) error {
	if _, err := s.db.Exec(`DELETE FROM _vibe_macros WHERE id = ?;`, id); err != nil {
		return fmt.Errorf("automation: failed to delete macro %q: %w", id, err)
	}
	return nil
}

// Replace reconciles a macro to exactly the supplied action set. Because the
// whole spec (including its action list) is stored as a single JSON blob, an
// upsert leaves no stale actions behind.
func (s *MacroStore) Replace(spec MacroSpec) error {
	return s.Save(spec)
}
