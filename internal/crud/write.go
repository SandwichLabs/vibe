package crud

import (
	"database/sql"
	"fmt"
)

// inTx runs one write in its own transaction: rolled back when fn fails,
// committed otherwise. what names the write in the commit error.
func inTx(db *sql.DB, what string, fn func(tx *sql.Tx) (*WriteResult, error)) (*WriteResult, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	res, err := fn(tx)
	if err != nil {
		tx.Rollback() //nolint:errcheck
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit %s: %w", what, err)
	}
	return res, nil
}

// InsertRow is InsertRowTx in its own transaction.
func InsertRow(db *sql.DB, colTypes map[string]string, table string, values map[string]any) (*WriteResult, error) {
	return inTx(db, "insert", func(tx *sql.Tx) (*WriteResult, error) {
		return InsertRowTx(tx, colTypes, table, values)
	})
}

// UpdateRow is UpdateRowTx in its own transaction.
func UpdateRow(db *sql.DB, colTypes map[string]string, table, pkCol string, pkVal any, values map[string]any) (*WriteResult, error) {
	return inTx(db, "update", func(tx *sql.Tx) (*WriteResult, error) {
		return UpdateRowTx(tx, colTypes, table, pkCol, pkVal, values)
	})
}

// DeleteRow is DeleteRowTx in its own transaction.
func DeleteRow(db *sql.DB, colTypes map[string]string, table, pkCol string, pkVal any) (*WriteResult, error) {
	return inTx(db, "delete", func(tx *sql.Tx) (*WriteResult, error) {
		return DeleteRowTx(tx, colTypes, table, pkCol, pkVal)
	})
}
