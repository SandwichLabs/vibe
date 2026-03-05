package config

import "time"

// Config holds application-wide configuration.
type Config struct {
	// Verbose enables debug logging.
	Verbose bool `mapstructure:"verbose"`

	// Editor is the external editor command for page editing.
	Editor string `mapstructure:"editor"`

	// Web server settings.
	Web WebConfig `mapstructure:"web"`

	// Agent settings.
	Agent AgentConfig `mapstructure:"agent"`

	// DuckDB settings.
	DuckDB DuckDBConfig `mapstructure:"duckdb"`
}

// WebConfig configures the embedded web preview server.
type WebConfig struct {
	Port     int    `mapstructure:"port"`
	Host     string `mapstructure:"host"`
	AllowSQL bool   `mapstructure:"allow_sql"`
}

// AgentConfig configures the AI agent interface.
type AgentConfig struct {
	Mode    string `mapstructure:"mode"`    // stdio | socket
	Cmd     string `mapstructure:"cmd"`     // agent command to spawn
	Listen  string `mapstructure:"listen"`  // socket/port to listen on
	Sandbox string `mapstructure:"sandbox"` // read-only | read-write | full
}

// DuckDBConfig configures DuckDB behavior.
type DuckDBConfig struct {
	MemoryLimit string `mapstructure:"memory_limit"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Editor: "vim",
		Web: WebConfig{
			Port: 3456,
			Host: "localhost",
		},
		Agent: AgentConfig{
			Mode:    "stdio",
			Sandbox: "read-only",
		},
		DuckDB: DuckDBConfig{
			MemoryLimit: "512MB",
		},
	}
}

// MetaKeys defines the required keys in _vibe_meta.
var MetaKeys = []string{
	"version",
	"format_version",
	"created_at",
	"updated_at",
	"title",
	"description",
}

// FormatVersion is the current .vibe format version.
const FormatVersion = "1.0.0"

// AppVersion is set at build time via ldflags.
var AppVersion = "dev"

// DefaultCacheDuration is the default cache duration for SQL block results.
const DefaultCacheDuration = 0 * time.Second
