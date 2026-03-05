package cmd

import (
	"fmt"

	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/spf13/cobra"
)

var dumpCmd = &cobra.Command{
	Use:   "dump <file>",
	Short: "Dump schema and pages to stdout",
	Long: `Dump the internal schema and page contents of a .vibe file to stdout
for debugging and backup purposes.`,
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

		// Dump schema (user tables).
		fmt.Fprintln(out, "-- Schema")
		fmt.Fprintln(out, "-- ======")
		rows, err := db.Query("SELECT name, sql FROM sqlite_master WHERE type='table' AND name NOT LIKE '_vibe_%' ORDER BY name;")
		if err != nil {
			return fmt.Errorf("failed to query schema: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var name, ddl string
			if err := rows.Scan(&name, &ddl); err != nil {
				return fmt.Errorf("failed to scan schema: %w", err)
			}
			fmt.Fprintf(out, "\n-- Table: %s\n%s;\n", name, ddl)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("failed to iterate schema: %w", err)
		}

		// Dump pages.
		fmt.Fprintln(out, "\n-- Pages")
		fmt.Fprintln(out, "-- =====")
		pageRows, err := db.Query("SELECT id, title, page_type, content FROM _vibe_pages ORDER BY sort_order, title;")
		if err != nil {
			return fmt.Errorf("failed to query pages: %w", err)
		}
		defer pageRows.Close()

		for pageRows.Next() {
			var id, title, pageType, content string
			if err := pageRows.Scan(&id, &title, &pageType, &content); err != nil {
				return fmt.Errorf("failed to scan page: %w", err)
			}
			fmt.Fprintf(out, "\n-- Page: %s (%s, type=%s)\n", title, id, pageType)
			fmt.Fprintln(out, content)
		}
		if err := pageRows.Err(); err != nil {
			return fmt.Errorf("failed to iterate pages: %w", err)
		}

		// Dump named queries.
		fmt.Fprintln(out, "\n-- Queries")
		fmt.Fprintln(out, "-- =======")
		queryRows, err := db.Query("SELECT id, title, engine, sql FROM _vibe_queries ORDER BY title;")
		if err != nil {
			return fmt.Errorf("failed to query queries: %w", err)
		}
		defer queryRows.Close()

		for queryRows.Next() {
			var id, title, eng, sql string
			if err := queryRows.Scan(&id, &title, &eng, &sql); err != nil {
				return fmt.Errorf("failed to scan query: %w", err)
			}
			fmt.Fprintf(out, "\n-- Query: %s (%s, engine=%s)\n%s;\n", title, id, eng, sql)
		}
		if err := queryRows.Err(); err != nil {
			return fmt.Errorf("failed to iterate queries: %w", err)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(dumpCmd)
}
