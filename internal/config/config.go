package config

import (
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

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

	// LLM provider/model settings for the in-app wizard agent.
	LLM LLMConfig `mapstructure:"llm"`

	// DuckDB settings.
	DuckDB DuckDBConfig `mapstructure:"duckdb"`
}

// LLM provider identifiers.
const (
	LLMProviderLocal     = "local"     // OpenAI-compatible server (e.g. local llama-server)
	LLMProviderAnthropic = "anthropic" // Anthropic Claude
	LLMProviderGemini    = "gemini"    // Google Gemini
)

// LLMConfig configures the in-app conversational wizard agent's model provider.
// It is app-global and viper-driven: set it in ~/.vibe.yaml under `llm:` or via
// VIBE_LLM_* environment variables. Cloud API keys are read from the standard
// provider env vars (ANTHROPIC_API_KEY, GEMINI_API_KEY) when APIKey is unset.
type LLMConfig struct {
	Provider string `mapstructure:"provider"` // local | anthropic | gemini
	Model    string `mapstructure:"model"`    // model id; empty = per-provider default
	Endpoint string `mapstructure:"endpoint"` // base URL for the local (OpenAI-compatible) provider
	APIKey   string `mapstructure:"api_key"`  // optional; else taken from the provider's standard env var
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
		LLM: LLMConfig{
			Provider: LLMProviderLocal,
			Endpoint: "http://localhost:8080/v1",
		},
		DuckDB: DuckDBConfig{
			MemoryLimit: "512MB",
		},
	}
}

// DefaultLLMEndpoint is the OpenAI-compatible base URL used by the local
// provider when none is configured.
const DefaultLLMEndpoint = "http://localhost:8080/v1"

// Load reads application configuration from (in increasing precedence): built-in
// defaults, ~/.vibe.yaml, and VIBE_* environment variables. It uses an isolated
// viper instance so it is safe to call from the Wails Studio binary, which does
// not run through the cobra/viper CLI init. Nested keys map to env vars by
// upper-snake-casing the path, e.g. llm.provider -> VIBE_LLM_PROVIDER.
//
// Cloud API keys fall back to the provider's standard env var when llm.api_key
// is unset, so secrets stay out of config files.
func Load() Config {
	cfg := DefaultConfig()

	v := viper.New()
	v.SetDefault("llm.provider", cfg.LLM.Provider)
	v.SetDefault("llm.endpoint", cfg.LLM.Endpoint)
	v.SetDefault("llm.model", cfg.LLM.Model)
	v.SetDefault("llm.api_key", cfg.LLM.APIKey)

	if home, err := os.UserHomeDir(); err == nil {
		v.AddConfigPath(home)
		v.SetConfigName(".vibe")
		v.SetConfigType("yaml")
	}
	v.SetEnvPrefix("VIBE")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	_ = v.ReadInConfig() // missing config file is fine; defaults + env still apply

	if err := v.Unmarshal(&cfg); err != nil {
		// On a malformed config, fall back to defaults rather than failing the app.
		return DefaultConfig()
	}

	cfg.LLM.resolve()
	return cfg
}

// resolve fills in provider-standard env-var API keys and per-provider default
// models when they are not explicitly configured.
func (l *LLMConfig) resolve() {
	if l.Provider == "" {
		l.Provider = LLMProviderLocal
	}
	if l.APIKey == "" {
		switch l.Provider {
		case LLMProviderAnthropic:
			l.APIKey = os.Getenv("ANTHROPIC_API_KEY")
		case LLMProviderGemini:
			if k := os.Getenv("GEMINI_API_KEY"); k != "" {
				l.APIKey = k
			} else {
				l.APIKey = os.Getenv("GOOGLE_API_KEY")
			}
		}
	}
	if l.Endpoint == "" {
		l.Endpoint = DefaultLLMEndpoint
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
