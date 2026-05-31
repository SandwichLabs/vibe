package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/spf13/cobra"
)

var infoCmd = &cobra.Command{
	Use:   "info <file>",
	Short: "Print metadata about a .vibe file",
	Long: `Display metadata for a .vibe file including title, format version,
page count, table count, and file size.

Use --json for machine-readable JSON output.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		filePath := args[0]
		out := cmd.OutOrStdout()
		jsonOutput, _ := cmd.Flags().GetBool("json")

		// Stat the file (no passphrase needed).
		fi, err := os.Stat(filePath)
		if err != nil {
			return fmt.Errorf("failed to stat file: %w", err)
		}

		// Prompt for passphrase to read metadata.
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

		// Collect metadata.
		meta := make(map[string]string)
		for _, key := range []string{"title", "description", "format_version", "version", "created_at", "updated_at"} {
			val, err := c.Meta(key)
			if err != nil || val == "" {
				continue
			}
			meta[key] = val
		}

		// Count pages.
		var pageCount int
		db.QueryRow("SELECT COUNT(*) FROM _vibe_pages;").Scan(&pageCount) //nolint:errcheck

		// Count user tables.
		var tableCount int
		db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE '_vibe_%';").Scan(&tableCount) //nolint:errcheck

		// Count queries.
		var queryCount int
		db.QueryRow("SELECT COUNT(*) FROM _vibe_queries;").Scan(&queryCount) //nolint:errcheck

		// Check DuckDB blob size.
		var duckdbSize int64
		db.QueryRow("SELECT COALESCE(LENGTH(blob), 0) FROM _vibe_duckdb LIMIT 1;").Scan(&duckdbSize) //nolint:errcheck

		if jsonOutput {
			result := map[string]any{
				"file":        filePath,
				"size":        fi.Size(),
				"size_human":  formatBytes(fi.Size()),
				"pages":       pageCount,
				"tables":      tableCount,
				"queries":     queryCount,
				"duckdb_size": duckdbSize,
			}
			for k, v := range meta {
				result[k] = v
			}
			enc := json.NewEncoder(out)
			enc.SetIndent("", "  ")
			return enc.Encode(result)
		}

		// Text output.
		fmt.Fprintf(out, "File:   %s\n", filePath)
		fmt.Fprintf(out, "Size:   %s\n", formatBytes(fi.Size()))

		for _, key := range []string{"title", "description", "format_version", "version", "created_at", "updated_at"} {
			val, ok := meta[key]
			if !ok {
				continue
			}
			fmt.Fprintf(out, "%-8s%s\n", key+":", val)
		}

		fmt.Fprintf(out, "Pages:  %d\n", pageCount)
		fmt.Fprintf(out, "Tables: %d\n", tableCount)
		fmt.Fprintf(out, "Queries: %d\n", queryCount)
		if duckdbSize > 0 {
			fmt.Fprintf(out, "DuckDB: %s\n", formatBytes(duckdbSize))
		}

		return nil
	},
}

func init() {
	infoCmd.Flags().Bool("json", false, "output metadata as JSON")
	rootCmd.AddCommand(infoCmd)
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
