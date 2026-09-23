package cmd

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sandwich-labs/vibe-app/internal/engine"
)

// importCSVIntoTable reads a CSV file and imports its contents into a table.
// If the table does not exist, it will be created with TEXT columns from the CSV header.
// Returns the number of rows imported.
func importCSVIntoTable(db *sql.DB, csvPath, tableName string) (int, error) {
	f, err := os.Open(csvPath)
	if err != nil {
		return 0, fmt.Errorf("failed to open CSV file: %w", err)
	}
	defer f.Close()

	reader := csv.NewReader(f)
	records, err := reader.ReadAll()
	if err != nil {
		return 0, fmt.Errorf("failed to read CSV: %w", err)
	}

	if len(records) < 1 {
		return 0, fmt.Errorf("CSV file is empty")
	}

	headers := records[0]

	// Create table if it doesn't exist.
	colDefs := make([]string, len(headers))
	for i, h := range headers {
		colDefs[i] = fmt.Sprintf("%s TEXT", engine.QuoteIdent(h))
	}
	createSQL := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s);", engine.QuoteIdent(tableName), strings.Join(colDefs, ", "))
	if _, err := db.Exec(createSQL); err != nil { //nolint:gosec
		return 0, fmt.Errorf("failed to create table: %w", err)
	}

	// Insert rows.
	placeholders := make([]string, len(headers))
	for i := range placeholders {
		placeholders[i] = "?"
	}
	insertSQL := fmt.Sprintf("INSERT INTO %s VALUES (%s);", engine.QuoteIdent(tableName), strings.Join(placeholders, ", "))

	tx, err := db.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}

	stmt, err := tx.Prepare(insertSQL) //nolint:gosec
	if err != nil {
		tx.Rollback() //nolint:errcheck
		return 0, fmt.Errorf("failed to prepare insert: %w", err)
	}
	defer stmt.Close()

	rowCount := 0
	for _, record := range records[1:] {
		vals := make([]any, len(record))
		for i, v := range record {
			vals[i] = v
		}
		if _, err := stmt.Exec(vals...); err != nil {
			tx.Rollback() //nolint:errcheck
			return 0, fmt.Errorf("failed to insert row %d: %w", rowCount+1, err)
		}
		rowCount++
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit: %w", err)
	}

	return rowCount, nil
}

// tableNameFromCSVPath derives a table name from the CSV file path.
func tableNameFromCSVPath(csvPath string) string {
	base := filepath.Base(csvPath)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	// Replace non-alphanumeric chars with underscores.
	var sb strings.Builder
	for _, c := range name {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
			sb.WriteRune(c)
		} else {
			sb.WriteRune('_')
		}
	}
	result := sb.String()
	if result == "" {
		return "imported"
	}
	return result
}
