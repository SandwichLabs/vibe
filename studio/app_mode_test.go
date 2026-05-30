package main

// TDD RED — Phase 4 / unit P4.3: "default_mode / startup metadata +
// GetMode/SetMode bindings" (parity plan §4.1/§4.3).
//
// REAL integration test: opens a real encrypted .vibe container (SQLCipher +
// DuckDB) and drives the production Wails-bound app-mode methods directly (no
// mocks), proving:
//
//   - default_mode is seeded as "studio" and GetAppConfig reports the live
//     mode as "studio" by default
//   - SetAppConfig persists default_mode/startup_object/app_title in _vibe_meta
//     and they survive a Save → Close → Open round-trip
//   - on (re)open the live session mode initializes from the persisted
//     default_mode (so a file saved with default_mode="app" opens in App Mode)
//   - SetMode flips the live runtime mode WITHOUT mutating the persisted
//     default_mode
//
// We avoid the production OpenFile here because it calls runtime.EventsEmit,
// which log.Fatalf's without a real Wails context. reopenAppMode mirrors the
// Save → Close → reopen lifecycle and then invokes the production
// initModeFromMeta — the exact hook OpenFile runs on open — so the
// "initialize live mode from persisted default_mode" behavior is asserted
// against real code.

import (
	"context"
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/sandwich-labs/vibe-app/internal/engine"
	"github.com/sandwich-labs/vibe-app/internal/queries"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reopenAppMode saves and closes the app's container, reopens it from the same
// path, re-wires the engine/registry, then runs the production mode-init hook
// (initModeFromMeta) — the same initialization OpenFile performs on launch.
func reopenAppMode(t *testing.T, app *App) {
	t.Helper()

	path := app.filePath
	require.NoError(t, app.container.Save())
	require.NoError(t, app.container.Close())

	c, err := container.Open(path,
		container.WithPassphrase(testPassphrase),
		container.WithLogger(app.logger),
	)
	require.NoError(t, err)

	app.container = c
	app.engine = engine.NewManager(c.DuckDBDB(), engine.WithLogger(app.logger))
	app.registry = queries.NewRegistry(c.SQLiteDB(), app.logger)
	app.ctx = context.Background()

	// Production hook: initialize the live session mode from persisted
	// default_mode (what OpenFile runs on file open).
	app.initModeFromMeta()

	t.Cleanup(func() { _ = c.Close() })
}

func TestAppModeDefaultsToStudio(t *testing.T) {
	app := newOpenApp(t)
	// Fresh container: live mode must initialize from the seeded default_mode.
	app.initModeFromMeta()

	cfg, err := app.GetAppConfig()
	require.NoError(t, err)

	assert.Equal(t, "studio", cfg.Mode, "live mode defaults to studio")
	assert.Equal(t, "studio", cfg.DefaultMode, "default_mode seeded as studio")

	// default_mode must be persisted in _vibe_meta (acceptance: seeded value).
	persisted, err := app.container.Meta("default_mode")
	require.NoError(t, err, "default_mode seeded in _vibe_meta")
	assert.Equal(t, "studio", persisted)
}

func TestAppModeSetAppConfigRoundTrip(t *testing.T) {
	app := newOpenApp(t)
	app.initModeFromMeta()

	require.NoError(t, app.SetAppConfig("app", "sw_main", "HVAC Jobs"))

	// Persisted immediately in _vibe_meta.
	dm, err := app.container.Meta("default_mode")
	require.NoError(t, err)
	assert.Equal(t, "app", dm)
	so, err := app.container.Meta("startup_object")
	require.NoError(t, err)
	assert.Equal(t, "sw_main", so)
	at, err := app.container.Meta("app_title")
	require.NoError(t, err)
	assert.Equal(t, "HVAC Jobs", at)

	// Round-trip: Save → Close → Open re-initializes live mode from persisted
	// default_mode and preserves startup_object/app_title.
	reopenAppMode(t, app)

	cfg, err := app.GetAppConfig()
	require.NoError(t, err)
	assert.Equal(t, "app", cfg.Mode, "reopened file initializes live mode from persisted default_mode")
	assert.Equal(t, "app", cfg.DefaultMode)
	assert.Equal(t, "sw_main", cfg.StartupObject, "startup_object survives round-trip")
	assert.Equal(t, "HVAC Jobs", cfg.AppTitle, "app_title survives round-trip")
}

func TestAppModeSetModeFlipsLiveOnly(t *testing.T) {
	app := newOpenApp(t)
	app.initModeFromMeta() // live mode = studio (from default_mode)

	// Flip the live runtime mode to app.
	require.NoError(t, app.SetMode("app"))

	cfg, err := app.GetAppConfig()
	require.NoError(t, err)
	assert.Equal(t, "app", cfg.Mode, "SetMode flips the live runtime mode")
	assert.Equal(t, "studio", cfg.DefaultMode, "SetMode does NOT mutate persisted default_mode")

	// Persisted default_mode must remain "studio".
	dm, err := app.container.Meta("default_mode")
	require.NoError(t, err)
	assert.Equal(t, "studio", dm, "default_mode unchanged after SetMode")

	// Flip back to studio.
	require.NoError(t, app.SetMode("studio"))
	cfg, err = app.GetAppConfig()
	require.NoError(t, err)
	assert.Equal(t, "studio", cfg.Mode)
}
