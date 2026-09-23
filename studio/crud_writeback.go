package main

// This file adapts the shared internal/crud write-back core to the
// package-local names used by the Studio bindings (app.go) and their tests.
// The transactional, type-coercing, validation-enforcing logic lives in
// internal/crud so the embedded HTTP server's form-submit pipeline can reuse
// the exact same write path.

import (
	"github.com/sandwich-labs/vibe-app/internal/crud"
)

// WriteResult and WriteError are re-exported from internal/crud so existing
// Studio code and tests keep their unqualified names.
type (
	WriteResult = crud.WriteResult
	WriteError  = crud.WriteError

	rowQuerier = crud.RowQuerier
	txExecer   = crud.TxExecer
)

func columnTypes(q rowQuerier, table string) (map[string]string, error) {
	return crud.ColumnTypes(q, table)
}

func fieldMetaMap(store *crud.FieldMetaStore, table string) (map[string]crud.FieldMeta, error) {
	return crud.FieldMetaMap(store, table)
}

func insertRowTx(tx txExecer, colTypes map[string]string, metas map[string]crud.FieldMeta, table string, values map[string]any) (*WriteResult, error) {
	return crud.InsertRowTx(tx, colTypes, metas, table, values)
}

func updateRowTx(tx txExecer, colTypes map[string]string, metas map[string]crud.FieldMeta, table, pkCol string, pkVal any, values map[string]any) (*WriteResult, error) {
	return crud.UpdateRowTx(tx, colTypes, metas, table, pkCol, pkVal, values)
}

func deleteRowTx(tx txExecer, colTypes map[string]string, table, pkCol string, pkVal any) (*WriteResult, error) {
	return crud.DeleteRowTx(tx, colTypes, table, pkCol, pkVal)
}
