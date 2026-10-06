package session

import (
	"log/slog"
	"sync/atomic"
	"time"
)

// Option configures New, Wrap and OpenDB.
type Option func(*config)

type config struct {
	logger        *slog.Logger
	tempDir       string        // DuckDB temp_directory (spill files)
	extensionDir  string        // app-private extension_directory
	secretDir     string        // secret_directory (SET; DSN ignores it)
	defaultTO     time.Duration // default statement timeout (0 = none)
	defaultRowCap int           // default row cap (0 = none)
	skipLock      bool          // tests only: leave lock_configuration off

	// initCatalog, once set by SetDefaultCatalog, is USEd on every new pooled
	// connection of an owned hub so the pool's default catalog follows it.
	initCatalog atomic.Pointer[string]
}

func newConfig(opts []Option) *config {
	c := &config{logger: slog.Default()}
	for _, o := range opts {
		o(c)
	}
	return c
}

// WithLogger sets the logger. The session never logs SQL text or attach
// parameters, only connection names and statuses.
func WithLogger(l *slog.Logger) Option {
	return func(c *config) {
		if l != nil {
			c.logger = l
		}
	}
}

// WithTempDirectory pins DuckDB's temp_directory (spill files). The caller
// owns the directory. Without it New creates a per-pid directory and removes
// it on Close.
func WithTempDirectory(dir string) Option { return func(c *config) { c.tempDir = dir } }

// WithExtensionDirectory pins the app-private extension_directory so
// extensions in the user's ~/.duckdb are never loadable (spike finding G).
func WithExtensionDirectory(dir string) Option { return func(c *config) { c.extensionDir = dir } }

// WithSecretDirectory pins secret_directory. Persistent secrets are disabled
// anyway; this is defence in depth, and it is SET (not DSN) because the DSN
// silently ignores it (spike finding J).
func WithSecretDirectory(dir string) Option { return func(c *config) { c.secretDir = dir } }

// WithDefaultTimeout sets the default per-statement timeout (0 = none).
func WithDefaultTimeout(d time.Duration) Option { return func(c *config) { c.defaultTO = d } }

// WithDefaultRowCap sets the default Rows cap (0 = none).
func WithDefaultRowCap(n int) Option { return func(c *config) { c.defaultRowCap = n } }

// QueryOption tunes one Query/Exec/Conn call.
type QueryOption func(*callConfig)

type callConfig struct {
	timeout    time.Duration
	hasTimeout bool
	rowCap     int
	hasRowCap  bool
	catalog    string
}

// WithTimeout overrides the statement timeout for one call (0 disables it).
func WithTimeout(d time.Duration) QueryOption {
	return func(c *callConfig) { c.timeout, c.hasTimeout = d, true }
}

// WithRowCap overrides the row cap for one Query (0 disables it).
func WithRowCap(n int) QueryOption {
	return func(c *callConfig) { c.rowCap, c.hasRowCap = n, true }
}

// WithCatalog runs the call with this catalog as the default for unqualified
// names, on a pinned connection (USE is per connection). Unset means the
// session default. Per-connection timeout/row-cap defaults of that catalog's
// Spec apply unless overridden.
func WithCatalog(name string) QueryOption { return func(c *callConfig) { c.catalog = name } }
