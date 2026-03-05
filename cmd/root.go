package cmd

import (
	"fmt"
	"log/slog"
	"os"

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
	Long: `Vibe is a portable, encrypted application container (.vibe files) that bundles
data, interactive pages, and an AI agent interface into a single file.

Two applications share one file format:
  - Vibe Studio (TUI) for creating and editing .vibe files
  - Vibe Viewer (GUI) for opening and interacting with .vibe files`,
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
