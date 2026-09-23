package cmd

import (
	"fmt"

	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/spf13/cobra"
)

var verifyCmd = &cobra.Command{
	Use:   "verify <file>",
	Short: "Verify the integrity of a .vibe file",
	Long: `Run integrity checks on a .vibe file including SQLite integrity_check,
schema validation, and metadata verification.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		filePath := args[0]

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
		out := cmd.OutOrStdout()
		errors := 0

		// 1. SQLite integrity check.
		fmt.Fprint(out, "SQLite integrity check... ")
		var result string
		if err := db.QueryRow("PRAGMA integrity_check;").Scan(&result); err != nil {
			fmt.Fprintf(out, "FAIL (%v)\n", err)
			errors++
		} else if result != "ok" {
			fmt.Fprintf(out, "FAIL (%s)\n", result)
			errors++
		} else {
			fmt.Fprintln(out, "OK")
		}

		// 2. Check required _vibe_ tables exist.
		requiredTables := []string{
			"_vibe_meta", "_vibe_pages", "_vibe_queries", "_vibe_components",
			"_vibe_agent_config", "_vibe_secrets",
			"_vibe_assets", "_vibe_duckdb", "_vibe_skills", "_vibe_notebooks",
		}
		fmt.Fprint(out, "Schema tables... ")
		var missingTables []string
		for _, table := range requiredTables {
			var count int
			err := db.QueryRow(
				"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?;", table).Scan(&count)
			if err != nil || count == 0 {
				missingTables = append(missingTables, table)
			}
		}
		if len(missingTables) > 0 {
			fmt.Fprintf(out, "FAIL (missing: %v)\n", missingTables)
			errors++
		} else {
			fmt.Fprintln(out, "OK")
		}

		// 3. Check required metadata keys.
		fmt.Fprint(out, "Metadata keys... ")
		requiredKeys := []string{"format_version", "created_at", "title"}
		var missingKeys []string
		for _, key := range requiredKeys {
			if _, err := c.Meta(key); err != nil {
				missingKeys = append(missingKeys, key)
			}
		}
		if len(missingKeys) > 0 {
			fmt.Fprintf(out, "FAIL (missing: %v)\n", missingKeys)
			errors++
		} else {
			fmt.Fprintln(out, "OK")
		}

		// 4. Check DuckDB blob.
		fmt.Fprint(out, "DuckDB blob... ")
		if ddb := c.DuckDBDB(); ddb != nil {
			if _, err := ddb.Exec("SELECT 1;"); err != nil {
				fmt.Fprintf(out, "FAIL (%v)\n", err)
				errors++
			} else {
				fmt.Fprintln(out, "OK")
			}
		} else {
			fmt.Fprintln(out, "OK (no blob)")
		}

		fmt.Fprintln(out)
		if errors > 0 {
			return fmt.Errorf("verification found %d error(s)", errors)
		}
		fmt.Fprintln(out, "All checks passed.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(verifyCmd)
}
