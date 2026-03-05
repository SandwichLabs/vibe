package cmd

import (
	"fmt"

	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/spf13/cobra"
)

var importCmd = &cobra.Command{
	Use:   "import <file>",
	Short: "Import data into a .vibe file",
	Long: `Import data from a CSV file into a table in a .vibe file.

If the table does not exist, it will be created with TEXT columns
derived from the CSV header.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		filePath := args[0]
		table, _ := cmd.Flags().GetString("table")
		csvFile, _ := cmd.Flags().GetString("csv")

		if table == "" {
			return fmt.Errorf("--table flag is required")
		}
		if csvFile == "" {
			return fmt.Errorf("--csv flag is required")
		}

		passphrase, err := promptPassphrase("Enter passphrase: ")
		if err != nil {
			return err
		}

		c, err := container.Open(filePath,
			container.WithPassphrase(passphrase),
			container.WithLogger(logger),
		)
		if err != nil {
			return fmt.Errorf("failed to open container: %w", err)
		}
		defer c.Close()

		rowCount, err := importCSVIntoTable(c.SQLiteDB(), csvFile, table)
		if err != nil {
			return err
		}

		// Save changes.
		if err := c.Save(); err != nil {
			return fmt.Errorf("failed to save: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Imported %d rows into %q\n", rowCount, table)
		return nil
	},
}

func init() {
	importCmd.Flags().String("table", "", "target table name (required)")
	importCmd.Flags().String("csv", "", "CSV file to import (required)")
	rootCmd.AddCommand(importCmd)
}
