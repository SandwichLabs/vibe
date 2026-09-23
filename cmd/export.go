package cmd

import (
	"encoding/csv"
	"encoding/json"
	"fmt"

	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/sandwich-labs/vibe-app/internal/engine"
	"github.com/spf13/cobra"
)

var exportCmd = &cobra.Command{
	Use:   "export <file>",
	Short: "Export table data from a .vibe file",
	Long: `Export data from a table in a .vibe file to CSV or JSON format.

Output is written to stdout.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		filePath := args[0]
		table, _ := cmd.Flags().GetString("table")
		format, _ := cmd.Flags().GetString("format")

		if table == "" {
			return fmt.Errorf("--table flag is required")
		}

		passphrase, err := promptPassphrase("Enter passphrase: ")
		if err != nil {
			return err
		}

		c, err := container.Open(filePath,
			container.WithPassphrase(passphrase),
			container.WithReadOnly(true),
			container.WithLogger(logger),
		)
		if err != nil {
			return fmt.Errorf("failed to open container: %w", err)
		}
		defer c.Close()

		db := c.SQLiteDB()

		rows, err := db.Query(fmt.Sprintf("SELECT * FROM %s;", engine.QuoteIdent(table))) //nolint:gosec
		if err != nil {
			return fmt.Errorf("failed to query table %q: %w", table, err)
		}
		defer rows.Close()

		columns, err := rows.Columns()
		if err != nil {
			return fmt.Errorf("failed to get columns: %w", err)
		}

		out := cmd.OutOrStdout()

		switch format {
		case "csv":
			w := csv.NewWriter(out)
			defer w.Flush()

			if err := w.Write(columns); err != nil {
				return fmt.Errorf("failed to write CSV header: %w", err)
			}

			for rows.Next() {
				values := make([]any, len(columns))
				ptrs := make([]any, len(columns))
				for i := range values {
					ptrs[i] = &values[i]
				}
				if err := rows.Scan(ptrs...); err != nil {
					return fmt.Errorf("failed to scan row: %w", err)
				}
				row := make([]string, len(columns))
				for i, v := range values {
					row[i] = fmt.Sprintf("%v", v)
				}
				if err := w.Write(row); err != nil {
					return fmt.Errorf("failed to write CSV row: %w", err)
				}
			}

		case "json":
			var result []map[string]any
			for rows.Next() {
				values := make([]any, len(columns))
				ptrs := make([]any, len(columns))
				for i := range values {
					ptrs[i] = &values[i]
				}
				if err := rows.Scan(ptrs...); err != nil {
					return fmt.Errorf("failed to scan row: %w", err)
				}
				row := make(map[string]any)
				for i, col := range columns {
					row[col] = values[i]
				}
				result = append(result, row)
			}

			enc := json.NewEncoder(out)
			enc.SetIndent("", "  ")
			if err := enc.Encode(result); err != nil {
				return fmt.Errorf("failed to encode JSON: %w", err)
			}

		default:
			return fmt.Errorf("unsupported format %q: use csv or json", format)
		}

		if err := rows.Err(); err != nil {
			return fmt.Errorf("failed to iterate rows: %w", err)
		}

		return nil
	},
}

func init() {
	exportCmd.Flags().String("table", "", "table name to export (required)")
	exportCmd.Flags().String("format", "csv", "output format: csv, json")
	rootCmd.AddCommand(exportCmd)
}
