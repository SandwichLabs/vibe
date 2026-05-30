// Package schema provides the Access-parity field-metadata layer (plan §1.3).
//
// It stores presentation/validation metadata (captions, control types,
// defaults, validation rules, input masks, lookup sources) for table columns
// in the SQLCipher container's _vibe_field_meta table, keyed by
// (table_name, column_name).
package schema

import (
	"database/sql"
	"fmt"
)

// FieldMeta holds Access-style field properties for a single table column.
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

// Store provides read/write access to field metadata in the SQLCipher container DB.
type Store struct {
	db *sql.DB
}

// NewStore constructs a field-metadata store over the given SQLCipher container DB.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// GetFieldMeta returns the metadata for every column of the given table,
// ordered by sort_order then column_name.
func (s *Store) GetFieldMeta(table string) ([]FieldMeta, error) {
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

// SetFieldMeta upserts the metadata for a single (table, column) pair.
func (s *Store) SetFieldMeta(table, column string, meta FieldMeta) error {
	required := 0
	if meta.Required {
		required = 1
	}

	_, err := s.db.Exec(
		`INSERT INTO _vibe_field_meta (
			table_name, column_name, caption, control_type, default_value,
			required, format_code, validation_rule, validation_message,
			input_mask, lookup_sql, display_col, value_col, control_tip, sort_order
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(table_name, column_name) DO UPDATE SET
			caption            = excluded.caption,
			control_type       = excluded.control_type,
			default_value      = excluded.default_value,
			required           = excluded.required,
			format_code        = excluded.format_code,
			validation_rule    = excluded.validation_rule,
			validation_message = excluded.validation_message,
			input_mask         = excluded.input_mask,
			lookup_sql         = excluded.lookup_sql,
			display_col        = excluded.display_col,
			value_col          = excluded.value_col,
			control_tip        = excluded.control_tip,
			sort_order         = excluded.sort_order;`,
		table, column, meta.Caption, meta.ControlType, meta.DefaultValue,
		required, meta.FormatCode, meta.ValidationRule, meta.ValidationMsg,
		meta.InputMask, meta.LookupSQL, meta.DisplayCol, meta.ValueCol, meta.ControlTip, meta.SortOrder,
	)
	if err != nil {
		return fmt.Errorf("failed to upsert field meta for %q.%q: %w", table, column, err)
	}
	return nil
}
