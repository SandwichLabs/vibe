package workspace

import (
	"errors"
	"fmt"
	"strings"

	"github.com/SandwichLabs/vibe/internal/sandbox"
)

// Settings is the Settings > Workspace form.
type Settings struct {
	Name              string `json:"name"`
	DefaultConnection string `json:"defaultConnection"`
	// TeamSandbox and TeamAskBeforeChanges are the committed limit (vibe.yaml).
	// "" or "full" means no tier limit; the gate flag true means required for
	// everyone.
	TeamSandbox          string `json:"teamSandbox"`
	TeamAskBeforeChanges bool   `json:"teamAskBeforeChanges"`
	// Sandbox and AskBeforeChanges are this person's own choice (kept outside
	// the folder).
	Sandbox          string `json:"sandbox"`
	AskBeforeChanges bool   `json:"askBeforeChanges"`
	// LoadedTeamSandbox and LoadedTeamAsk are the committed values the form was
	// opened with. When sent, a vibe.yaml that has changed since (a git pull) makes
	// the save refuse instead of silently overwriting the team's change.
	LoadedTeamSandbox *string `json:"loadedTeamSandbox,omitempty"`
	LoadedTeamAsk     *bool   `json:"loadedTeamAsk,omitempty"`
}

// SettingsConflictError is a settings save refused because vibe.yaml moved
// since the form was opened. It matches ErrConflict.
type SettingsConflictError struct{ Msg string }

func (e *SettingsConflictError) Error() string { return e.Msg }

// Is makes errors.Is(err, ErrConflict) true.
func (e *SettingsConflictError) Is(target error) bool { return target == ErrConflict }

// ParseSandbox strictly parses s as one of the three sandbox tiers, for callers
// that must reject anything else outright rather than fail closed on a
// possibly-stale value.
func ParseSandbox(s string) (string, error) {
	m, err := sandbox.Parse(s)
	return string(m), err
}

// ApplySettings saves the Settings > Workspace form: the name, default
// connection and team limit go into vibe.yaml (shared through git), and this
// person's own agent choice goes into the per-person file outside the folder.
//
// vibe.yaml can only tighten the agent: a person's tier above the team limit is
// refused, and so is turning the ask-before-changes gate off when the team
// requires it. Settings loaded before a vibe.yaml change (LoadedTeam*), or a
// vibe.yaml that changed on disk, are refused with a *SettingsConflictError
// before anything is written. Nothing is written when validation fails.
func ApplySettings(ws Workspace, s Settings) error {
	team := s.TeamSandbox
	if team == SandboxFull {
		team = "" // no limit
	}
	mode, err := validateSettings(ws, s, team)
	if err != nil {
		return err
	}
	cfg := ws.Config()
	if staleTeamForm(cfg, s) {
		return &SettingsConflictError{Msg: "the team's agent limit in vibe.yaml changed since Settings was opened. Reload the page and try again."}
	}
	next := cfg
	next.Name = strings.TrimSpace(s.Name)
	next.DefaultConnection = s.DefaultConnection
	next.Sandbox = team
	next.AskBeforeWrites = nil
	if s.TeamAskBeforeChanges {
		next.AskBeforeWrites = Bool(true)
	}
	if !configEqual(cfg, next) {
		if err := ws.SaveConfig(next); err != nil {
			if errors.Is(err, ErrConflict) {
				return &SettingsConflictError{Msg: "vibe.yaml changed on disk since Settings was opened. Reload and try again."}
			}
			return err
		}
	}
	return SetLocalAgent(ws, mode, s.AskBeforeChanges)
}

// validateSettings checks the form against the rules and returns the person's
// tier. team is the team limit with "full" already read as no limit.
func validateSettings(ws Workspace, s Settings, team string) (string, error) {
	if err := ValidateSandbox(team); err != nil {
		return "", err
	}
	if s.DefaultConnection != "" {
		if _, found := ws.Connection(s.DefaultConnection); !found {
			return "", fmt.Errorf("There's no connection called %q.", s.DefaultConnection)
		}
	}
	mode, err := ParseSandbox(s.Sandbox)
	if err != nil {
		return "", err
	}
	if team != "" && rank(mode) > rank(team) {
		return "", fmt.Errorf("The team limit is %s, so this computer can't go above it.", team)
	}
	if !s.AskBeforeChanges && s.TeamAskBeforeChanges {
		return "", errors.New("When the team requires the agent to ask before changes, it can't be turned off on this computer.")
	}
	if !ws.LocalTrusted() {
		return "", errors.New("Your saved settings for this workspace were made for a different folder. Confirm them first.")
	}
	return mode, nil
}

// staleTeamForm reports whether the committed limit differs from the one the
// form was opened with (it only counts when the form said what it loaded).
func staleTeamForm(cfg Config, s Settings) bool {
	if s.LoadedTeamSandbox == nil && s.LoadedTeamAsk == nil {
		return false
	}
	curSandbox := cfg.Sandbox
	if curSandbox == SandboxFull {
		curSandbox = ""
	}
	curAsk := cfg.AskBeforeWrites != nil && *cfg.AskBeforeWrites
	return (s.LoadedTeamSandbox != nil && *s.LoadedTeamSandbox != curSandbox) || (s.LoadedTeamAsk != nil && *s.LoadedTeamAsk != curAsk)
}

// SetLocalAgent saves this person's own agent tier and gate, kept outside the
// folder. The tier may not exceed the committed team limit, and the gate may not
// be turned off when vibe.yaml requires it (checked against the config as
// saved, so a form that validated against an older vibe.yaml is still caught).
func SetLocalAgent(ws Workspace, sandbox string, ask bool) error {
	mode, err := ParseSandbox(sandbox)
	if err != nil {
		return err
	}
	cfg := ws.Config()
	if cfg.Sandbox != "" && rank(mode) > rank(cfg.Sandbox) {
		return fmt.Errorf("The team limit in vibe.yaml is %s, so this computer can't go above it.", cfg.Sandbox)
	}
	if !ask && cfg.AskBeforeWrites != nil && *cfg.AskBeforeWrites {
		return errors.New("vibe.yaml requires the agent to ask before changes, so this can't be turned off on this computer.")
	}
	local := ws.Local()
	local.Agent.Sandbox = mode
	if ask {
		local.Agent.AskBeforeWrites = nil
	} else {
		local.Agent.AskBeforeWrites = Bool(false)
	}
	if err := ws.SaveLocal(local); err != nil {
		if errors.Is(err, ErrLocalUntrusted) {
			return errors.New("Your saved settings for this workspace were made for a different folder. Confirm them first.")
		}
		return fmt.Errorf("failed to save your settings: %w", err)
	}
	return nil
}

func configEqual(a, b Config) bool {
	boolEq := func(x, y *bool) bool {
		if x == nil || y == nil {
			return x == nil && y == nil
		}
		return *x == *y
	}
	return a.Name == b.Name && a.DefaultConnection == b.DefaultConnection &&
		a.Sandbox == b.Sandbox && boolEq(a.AskBeforeWrites, b.AskBeforeWrites)
}
