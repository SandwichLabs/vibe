package main

// Phase 4 / unit P4.3: "default_mode / startup metadata + GetMode/SetMode
// bindings" (parity plan §4.1/§4.3).
//
// The app shell runs in one of two modes — "studio" (builder) or "app"
// (locked-down data entry). The LIVE runtime mode is held on the App struct
// (a.mode) and may be flipped at runtime via SetMode without touching the file.
// The PERSISTED startup configuration (default_mode, startup_object, app_title)
// lives in _vibe_meta and survives Save → Close → Open. On file open the live
// mode initializes from the persisted default_mode via initModeFromMeta.

import "fmt"

// modeStudio / modeApp are the two valid app-shell modes.
const (
	modeStudio = "studio"
	modeApp    = "app"
)

// AppConfig is the combined live + persisted app-mode configuration returned by
// GetAppConfig. Mode is the live runtime mode held on the App struct;
// DefaultMode/StartupObject/AppTitle are persisted in _vibe_meta.
type AppConfig struct {
	Mode          string `json:"mode"`
	DefaultMode   string `json:"default_mode"`
	StartupObject string `json:"startup_object"`
	AppTitle      string `json:"app_title"`
}

// GetAppConfig returns the combined live + persisted app-mode configuration.
func (a *App) GetAppConfig() (AppConfig, error) {
	if a.container == nil {
		return AppConfig{}, fmt.Errorf("failed to get app config: no file open")
	}

	cfg := AppConfig{Mode: a.mode}

	defaultMode, err := a.container.Meta("default_mode")
	if err != nil {
		return AppConfig{}, fmt.Errorf("failed to get app config: %w", err)
	}
	cfg.DefaultMode = defaultMode

	// startup_object and app_title are optional; absence is not an error.
	if so, err := a.container.Meta("startup_object"); err == nil {
		cfg.StartupObject = so
	}
	if at, err := a.container.Meta("app_title"); err == nil {
		cfg.AppTitle = at
	}

	return cfg, nil
}

// SetMode flips the live runtime mode ("studio" | "app") without mutating the
// persisted default_mode. Capability gating (P4.4) and the App Shell UI (P4.5)
// key off the live mode.
func (a *App) SetMode(mode string) error {
	if mode != modeStudio && mode != modeApp {
		return fmt.Errorf("failed to set mode: invalid mode %q", mode)
	}
	a.mode = mode
	a.logger.Info("app mode flipped", "mode", mode)
	return nil
}

// SetAppConfig persists default_mode, startup_object, and app_title in
// _vibe_meta. The live runtime mode is left to SetMode/initModeFromMeta.
func (a *App) SetAppConfig(defaultMode, startupObject, appTitle string) error {
	if a.container == nil {
		return fmt.Errorf("failed to set app config: no file open")
	}
	if defaultMode != modeStudio && defaultMode != modeApp {
		return fmt.Errorf("failed to set app config: invalid default_mode %q", defaultMode)
	}

	if err := a.container.SetMeta("default_mode", defaultMode); err != nil {
		return fmt.Errorf("failed to set app config: %w", err)
	}
	if err := a.container.SetMeta("startup_object", startupObject); err != nil {
		return fmt.Errorf("failed to set app config: %w", err)
	}
	if err := a.container.SetMeta("app_title", appTitle); err != nil {
		return fmt.Errorf("failed to set app config: %w", err)
	}

	a.logger.Info("app config saved",
		"default_mode", defaultMode,
		"startup_object", startupObject,
		"app_title", appTitle,
	)
	return nil
}

// initModeFromMeta initializes the live a.mode from the persisted default_mode.
// Invoked on file open (OpenFile/CreateFile). Falls back to "studio" if the
// persisted value is missing or invalid so the shell always has a valid mode.
func (a *App) initModeFromMeta() {
	a.mode = modeStudio
	if a.container == nil {
		return
	}
	if dm, err := a.container.Meta("default_mode"); err == nil && (dm == modeStudio || dm == modeApp) {
		a.mode = dm
	}
}
