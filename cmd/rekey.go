package cmd

import (
	"fmt"

	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/spf13/cobra"
)

var rekeyCmd = &cobra.Command{
	Use:   "rekey <file>",
	Short: "Change the passphrase of a .vibe file",
	Long: `Change the encryption passphrase for a .vibe file.

Prompts for the current passphrase and then the new passphrase.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		filePath := args[0]

		// Get current passphrase.
		currentPass, err := promptPassphrase("Current passphrase: ")
		if err != nil {
			return err
		}

		// Open with current passphrase.
		c, err := container.Open(filePath,
			container.WithPassphrase(currentPass),
			container.WithLogger(logger),
		)
		if err != nil {
			return err
		}
		defer c.Close()

		// Get new passphrase.
		newPass, err := promptPassphraseConfirm()
		if err != nil {
			return err
		}

		// Execute PRAGMA rekey.
		db := c.SQLiteDB()
		if _, err := db.Exec(fmt.Sprintf("PRAGMA rekey = '%s';", newPass)); err != nil {
			return fmt.Errorf("failed to rekey: %w", err)
		}

		fmt.Fprintln(cmd.OutOrStdout(), "Passphrase changed successfully.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(rekeyCmd)
}
