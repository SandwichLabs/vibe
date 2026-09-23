package cmd

import (
	"fmt"

	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/spf13/cobra"
)

var dumpCmd = &cobra.Command{
	Use:   "dump <file>",
	Short: "Dump schema, notebooks, and skills to stdout",
	Long: `Dump the internal schema, notebooks, skills, named queries, and any
unmigrated legacy pages of a .vibe file to stdout for debugging and backup
purposes.`,
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

		// Dump notebooks (#66 — the primary content surface, replacing pages).
		fmt.Fprintln(out, "\n-- Notebooks")
		fmt.Fprintln(out, "-- =========")
		nbRows, err := db.Query("SELECT id, title, cells FROM _vibe_notebooks ORDER BY title;")
		if err != nil {
			return fmt.Errorf("failed to query notebooks: %w", err)
		}
		defer nbRows.Close()

		for nbRows.Next() {
			var id, title, cellsJSON string
			if err := nbRows.Scan(&id, &title, &cellsJSON); err != nil {
				return fmt.Errorf("failed to scan notebook: %w", err)
			}
			fmt.Fprintf(out, "\n-- Notebook: %s (%s)\n%s\n", title, id, cellsJSON)
		}
		if err := nbRows.Err(); err != nil {
			return fmt.Errorf("failed to iterate notebooks: %w", err)
		}

		// Dump skills (#62 — user/import-authored; built-ins are embedded in
		// the binary, not stored per-file, so they aren't dumped here).
		fmt.Fprintln(out, "\n-- Skills")
		fmt.Fprintln(out, "-- ======")
		skillRows, err := db.Query("SELECT name, description, instructions, table_name, source FROM _vibe_skills ORDER BY name;")
		if err != nil {
			return fmt.Errorf("failed to query skills: %w", err)
		}
		defer skillRows.Close()

		for skillRows.Next() {
			var name, description, instructions, tableName, source string
			if err := skillRows.Scan(&name, &description, &instructions, &tableName, &source); err != nil {
				return fmt.Errorf("failed to scan skill: %w", err)
			}
			fmt.Fprintf(out, "\n-- Skill: %s (source=%s", name, source)
			if tableName != "" {
				fmt.Fprintf(out, ", table=%s", tableName)
			}
			fmt.Fprintf(out, ")\n-- %s\n%s\n", description, instructions)
		}
		if err := skillRows.Err(); err != nil {
			return fmt.Errorf("failed to iterate skills: %w", err)
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

		// Dump any legacy pages that have not yet been migrated to a notebook
		// (info opens read-only, so MigrateOnOpen never runs — see its doc
		// comment). A fully-migrated file has none of these.
		fmt.Fprintln(out, "\n-- Legacy pages (unmigrated)")
		fmt.Fprintln(out, "-- =========================")
		pageRows, err := db.Query("SELECT id, title, page_type, content FROM _vibe_pages WHERE migrated_to = '' ORDER BY sort_order, title;")
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

		return nil
	},
}

func init() {
	rootCmd.AddCommand(dumpCmd)
}
