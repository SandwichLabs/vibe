// Package cmd holds the Cobra command tree for the vibe CLI (workspace, query,
// import, export, conn, config, bundle, mcp, studio).
package cmd

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile string
	logger  *slog.Logger
	// cliRedactor scrubs credential-shaped text from the log and from
	// workspace sessions' errors; the CLI resolves no secret values itself.
	cliRedactor = secrets.NewRedactor()

	// Version info set via ldflags.
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

var rootCmd = &cobra.Command{
	Use:   "vibe",
	Short: "Vibe — an agentic desktop workspace for working with data where it lives",
	Long: `Vibe is an agentic workspace for data that stays where it already lives:
local DuckDB files, MotherDuck, Postgres, SQLite, object storage, all reached
through one DuckDB hub. A workspace is a folder (optionally a git repository)
holding notebooks, saved queries and skills as plain files, plus a vibe.yaml
that lists its connections. Credentials live in your OS keychain or
environment, never in the folder. The workspace is exposed to the Studio
desktop app and to external agents over MCP (` + "`vibe mcp serve`" + `).

Start with ` + "`vibe workspace init`" + `, then ` + "`vibe query`" + `, ` + "`vibe import`" + `,
` + "`vibe export`" + `, ` + "`vibe studio`" + ` (the terminal UI; --gui for the desktop app)
and ` + "`vibe mcp serve`" + `.`,
	Version: fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date),
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.vibe.yaml)")
	rootCmd.PersistentFlags().Bool("verbose", false, "enable verbose logging")
	_ = viper.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose"))
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		viper.AddConfigPath(home)
		viper.SetConfigType("yaml")
		viper.SetConfigName(".vibe")
	}

	viper.SetEnvPrefix("VIBE")
	viper.AutomaticEnv()

	// Read config file if it exists; ignore "not found" errors.
	_ = viper.ReadInConfig()

	// Initialize logger.
	// A CLI prints its own messages: the structured log stays quiet unless
	// --verbose, so data commands do not spray JSON on a terminal.
	level := slog.LevelError
	if viper.GetBool("verbose") {
		level = slog.LevelDebug
	}
	logger = slog.New(secrets.NewRedactingHandler(
		slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}), cliRedactor))
}
