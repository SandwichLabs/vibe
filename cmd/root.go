package cmd

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile string
	logger  *slog.Logger

	// Version info set via ldflags.
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

var rootCmd = &cobra.Command{
	Use:   "vibe",
	Short: "Vibe — portable, encrypted application containers",
	Long: `Vibe is a local-first, encrypted, single-file agentic data workspace. A
.vibe file bundles DuckDB tables, notebooks (SQL/markdown/A2UI cells),
skills (domain context for an AI agent), and the agent itself into one
portable, passphrase-protected file — exposed to the Studio desktop app and
to external agents over MCP (` + "`vibe mcp serve`" + `).

Two applications share one file format:
  - Vibe Studio (TUI) — a SQL REPL, a read-only table browser, and a
    notebook list, for quick terminal access to a .vibe file
  - Vibe Studio (GUI, ` + "`vibe studio --gui`" + `) — the full Wails desktop app
    for authoring notebooks, tables, skills, and talking to the agent`,
	Version: fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date),
	// PersistentPreRun runs after cobra.OnInitialize (initConfig) has wired the
	// logger, so orphan-temp recovery from crashed processes happens once per
	// CLI invocation, before any command opens a container. Best-effort: it logs
	// but never blocks command execution.
	PersistentPreRun: func(_ *cobra.Command, _ []string) {
		container.StartupSweep(logger)
	},
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
	level := slog.LevelInfo
	if viper.GetBool("verbose") {
		level = slog.LevelDebug
	}
	logger = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}
