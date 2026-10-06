package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// The container commands are gone with the .vibe container (#113). Each name
// stays registered, hidden, so someone with muscle memory (or a script) gets a
// pointer instead of "unknown command".
func init() {
	for _, name := range []string{"info", "verify", "compact", "rekey", "dump", "passphrase"} {
		name := name
		rootCmd.AddCommand(&cobra.Command{
			Use:                name,
			Hidden:             true,
			DisableFlagParsing: true,
			SilenceUsage:       true,
			RunE: func(_ *cobra.Command, _ []string) error {
				return fmt.Errorf("`vibe %s` worked on .vibe container files, which are no longer opened; use `vibe workspace init` to create a workspace and `vibe workspace info` to inspect one", name)
			},
		})
	}
}
