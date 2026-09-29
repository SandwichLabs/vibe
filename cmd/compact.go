package cmd

import (
	"fmt"

	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/spf13/cobra"
)

var compactCmd = &cobra.Command{
	Use:   "compact <file>",
	Short: "Compact a .vibe file (VACUUM both engines)",
	Long: `Run VACUUM on both the SQLite and DuckDB engines within a .vibe file
to reclaim unused space and optimize storage.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		filePath := args[0]

		passphrase, err := promptPassphrase("Enter passphrase: ")
		if err != nil {
			return err
		}

		c, err := container.Open(filePath,
			container.WithPassphrase(passphrase),
			container.WithLogger(logger),
		)
		if err != nil {
			return err
		}
		defer c.Close()

		out := cmd.OutOrStdout()

		// VACUUM SQLite.
		fmt.Fprintln(out, "Compacting SQLite...")
		if _, err := c.SQLiteDB().Exec("VACUUM;"); err != nil {
			return fmt.Errorf("failed to vacuum SQLite: %w", err)
		}

		// VACUUM DuckDB if available.
		if ddb := c.DuckDBDB(); ddb != nil {
			fmt.Fprintln(out, "Compacting DuckDB...")
			if _, err := ddb.Exec("CHECKPOINT;"); err != nil {
				logger.Warn("DuckDB checkpoint failed", "error", err)
			}
		}

		// Save to persist the compacted state.
		if err := c.Save(); err != nil {
			return fmt.Errorf("failed to save: %w", err)
		}

		fmt.Fprintln(out, "Compaction complete.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(compactCmd)
}
