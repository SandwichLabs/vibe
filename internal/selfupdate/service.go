package selfupdate

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/SandwichLabs/vibe/internal/prefs"
)

// autoCheckInterval is how long a weekly check waits.
const autoCheckInterval = 7 * 24 * time.Hour

// applyTimeout bounds a whole install (install.sh's own downloads have no timeout of their own).
const applyTimeout = 10 * time.Minute

// SettingsStore is where the person's update answer lives (a *prefs.Recents).
type SettingsStore interface {
	UpdateSettings() prefs.UpdateSettings
	ModifyUpdateSettings(fn func(*prefs.UpdateSettings)) error
}

// Service joins the Updater to the person's saved choice. Studio and the CLI both use it,
// so "ask once", "weekly" and "skip" behave the same everywhere.
type Service struct {
	u      *Updater
	store  SettingsStore
	logger *slog.Logger
	now    func() time.Time
	// applyTimeout bounds Apply (a field so a test can shorten it).
	applyTimeout time.Duration

	mu         sync.Mutex
	applied    string // version installed in this process's lifetime and not yet running ("0.1.2")
	canRestart bool
}

// NewService returns a Service. logger may be nil.
func NewService(u *Updater, store SettingsStore, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Service{u: u, store: store, logger: logger, now: time.Now, applyTimeout: applyTimeout}
}

// State is everything a UI needs to draw the update controls without touching the network.
type State struct {
	prefs.UpdateSettings
	Version string `json:"version"`
	// IsDev: an unstamped build. It never asks and never reports an update.
	IsDev bool `json:"isDev"`
	// NeedsConsent: the person was never asked and this is a release build.
	NeedsConsent bool   `json:"needsConsent"`
	Managed      string `json:"managed"`
	Updatable    bool   `json:"updatable"`
	ManagedNote  string `json:"managedNote"`
	// AppliedVersion is set after an install finished in this session: the running program is
	// still the old version until it restarts. CanRestart says a Studio restart is possible.
	AppliedVersion string `json:"appliedVersion"`
	CanRestart     bool   `json:"canRestart"`
}

// State reads the saved choice and the install kind.
func (s *Service) State() State {
	set := s.store.UpdateSettings()
	st := State{UpdateSettings: set, Version: s.u.version, IsDev: s.u.IsDev()}
	// A dev build with the QA hook active behaves like a release build so the UI can be exercised.
	s.mu.Lock()
	st.AppliedVersion, st.CanRestart = s.applied, s.canRestart
	s.mu.Unlock()
	st.NeedsConsent = set.Check == "" && (!st.IsDev || s.u.current != s.u.version)
	if _, err := s.u.Detect(); err == nil {
		st.Managed, st.Updatable = ManagedInstaller, true
	} else {
		st.Managed, st.ManagedNote = ManagedExternal, ExternalNote
	}
	return st
}

// SetCheck saves the answer: "off" or "weekly".
func (s *Service) SetCheck(mode string) error {
	if mode != prefs.UpdateCheckOff && mode != prefs.UpdateCheckWeekly {
		return fmt.Errorf("unknown setting %q (want off or weekly)", mode)
	}
	return s.store.ModifyUpdateSettings(func(u *prefs.UpdateSettings) { u.Check = mode })
}

// CheckNow checks regardless of the saved choice: asking is the consent for this one request.
func (s *Service) CheckNow(ctx context.Context) (Result, error) {
	set := s.store.UpdateSettings()
	res, err := s.u.Check(ctx, set.SkippedVersion)
	if err != nil {
		return Result{}, err
	}
	s.recordChecked(res.CheckedAt)
	return res, nil
}

// AutoCheckDue reports whether a startup check should run: the person chose weekly,
// the last check is over a week old (or never) and the build is not a plain dev build.
func (s *Service) AutoCheckDue() bool {
	set := s.store.UpdateSettings()
	if set.Check != prefs.UpdateCheckWeekly || (s.u.IsDev() && s.u.current == s.u.version) {
		return false
	}
	last, err := time.Parse(time.RFC3339, set.LastChecked)
	return err != nil || s.now().Sub(last) >= autoCheckInterval
}

// AutoCheck runs the unprompted weekly check. It returns nil when none was due. A failure is
// logged quietly and returned as nil: it is never shown to the person.
func (s *Service) AutoCheck(ctx context.Context) *Result {
	if !s.AutoCheckDue() {
		return nil
	}
	res, err := s.CheckNow(ctx)
	if err != nil {
		s.logger.Info("update check failed", "error", err)
		return nil
	}
	return &res
}

// Skip remembers tag as skipped; an empty tag clears it.
func (s *Service) Skip(tag string) error {
	if tag != "" && !ValidTag(tag) {
		return fmt.Errorf("%w: %q", ErrBadTag, tag)
	}
	return s.store.ModifyUpdateSettings(func(u *prefs.UpdateSettings) { u.SkippedVersion = tag })
}

// Apply installs tag (see Updater.Apply) and clears a skip for it.
func (s *Service) Apply(ctx context.Context, tag string) (ApplyResult, error) {
	ctx, cancel := context.WithTimeout(ctx, s.applyTimeout)
	defer cancel()
	res, err := s.u.Apply(ctx, tag)
	if err == nil {
		_ = s.Skip("")
		s.mu.Lock()
		s.applied, s.canRestart = res.To, res.Restart != ""
		s.mu.Unlock()
	}
	return res, err
}

func (s *Service) recordChecked(at string) {
	if err := s.store.ModifyUpdateSettings(func(u *prefs.UpdateSettings) { u.LastChecked = at }); err != nil {
		s.logger.Debug("failed to record the update check time", "error", err)
	}
}
