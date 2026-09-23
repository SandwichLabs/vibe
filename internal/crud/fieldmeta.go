package crud

// Field-metadata read support for CRUD validation (required + validation_rule
// enforcement). Ported from the (now-removed) internal/schema package's
// fieldmeta.go as part of #68 (Access application layer removal): the bound
// forms that authored _vibe_field_meta rows are gone, but CRUD's server-side
// required/validation_rule checks are datasheet-editing features that stay,
// and an OLD .vibe file may still carry _vibe_field_meta rows from before
// #68 (additive-migration policy: existing tables are never dropped). New
// files never create the table, so GetFieldMeta tolerates it being absent
// entirely, returning no metadata rather than an error.

import (
	"database/sql"
	"fmt"
	"strings"
)

// FieldMeta holds presentation/validation metadata for a single table column:
// caption, required flag, and a SQL validation_rule/validation_message pair.
type FieldMeta struct {
	TableName      string `json:"table_name"`
	ColumnName     string `json:"column_name"`
	Caption        string `json:"caption"`
	ControlType    string `json:"control_type"`
	DefaultValue   string `json:"default_value"`
	Required       bool   `json:"required"`
	FormatCode     string `json:"format_code"`
	ValidationRule string `json:"validation_rule"`
	ValidationMsg  string `json:"validation_message"`
	InputMask      string `json:"input_mask"`
	LookupSQL      string `json:"lookup_sql"`
	DisplayCol     string `json:"display_col"`
	ValueCol       string `json:"value_col"`
	ControlTip     string `json:"control_tip"`
	SortOrder      int    `json:"sort_order"`
}

// FieldMetaStore provides read access to field metadata in the SQLCipher
// container DB. Write access was removed with the form/schema builders that
// used to author it (#68); nothing writes _vibe_field_meta any more.
type FieldMetaStore struct {
	db *sql.DB
}

// NewFieldMetaStore constructs a field-metadata store over the given
// SQLCipher container DB.
func NewFieldMetaStore(db *sql.DB) *FieldMetaStore {
	return &FieldMetaStore{db: db}
}

// GetFieldMeta returns the metadata for every column of the given table,
// ordered by sort_order then column_name. Tolerates a missing/empty meta
// table (a new file, or one created after #68) by returning no rows.
func (s *FieldMetaStore) GetFieldMeta(table string) ([]FieldMeta, error) {
	rows, err := s.db.Query(
		`SELECT table_name, column_name, caption, control_type, default_value,
		        required, format_code, validation_rule, validation_message,
		        input_mask, lookup_sql, display_col, value_col, control_tip, sort_order
		   FROM _vibe_field_meta
		  WHERE table_name = ?
		  ORDER BY sort_order, column_name;`,
		table,
	)
	if err != nil {
		if isMissingTable(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query field meta for table %q: %w", table, err)
	}
	defer rows.Close() //nolint:errcheck

	var out []FieldMeta
	for rows.Next() {
		var fm FieldMeta
		var required int
		if err := rows.Scan(
			&fm.TableName, &fm.ColumnName, &fm.Caption, &fm.ControlType, &fm.DefaultValue,
			&required, &fm.FormatCode, &fm.ValidationRule, &fm.ValidationMsg,
			&fm.InputMask, &fm.LookupSQL, &fm.DisplayCol, &fm.ValueCol, &fm.ControlTip, &fm.SortOrder,
		); err != nil {
			return nil, fmt.Errorf("failed to scan field meta for table %q: %w", table, err)
		}
		fm.Required = required != 0
		out = append(out, fm)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate field meta for table %q: %w", table, err)
	}
	return out, nil
}

// isMissingTable reports whether err is a SQLite "no such table" error.
func isMissingTable(err error) bool {
	return strings.Contains(err.Error(), "no such table")
}
