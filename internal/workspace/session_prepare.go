package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/SandwichLabs/vibe/internal/connections"
	"github.com/SandwichLabs/vibe/internal/session"
)

// preparer is the per-type step of OpenSession: what has to be true of one
// resolved connection of a type before it is registered. OpenSession looks the
// preparer up by connection type (defaultPreparers) and runs it, so the
// per-type differences live in one place instead of OpenSession branching on
// the type.
type preparer interface {
	// ReadOnly returns the effective read-only flag for rc; forced is true for
	// a process that does not own the hub. An error leaves the connection out
	// of the session.
	ReadOnly(rc ResolvedConnection, forced bool) (bool, error)
	// Prepare validates def and readies whatever spec needs on disk. An error
	// leaves the connection out of the session; env.Fail reports a problem that
	// does not.
	Prepare(ctx context.Context, env prepareEnv, rc ResolvedConnection, def connections.Def, spec *session.Spec) error
	// Eager reports whether the connection is attached when the session opens
	// (a local file, nothing to dial) instead of on first use.
	Eager() bool
}

// prepareEnv is what OpenSession hands every preparer.
type prepareEnv struct {
	// Root is the workspace folder.
	Root string
	// ForceReadOnly and SkipLocal mirror WithForceReadOnly and WithSkipLocalFiles.
	ForceReadOnly, SkipLocal bool
	// Fail reports a problem with a connection that stays in the session.
	Fail func(name, msg string)
	// ClaimHubToken offers the MotherDuck token a connection resolved; it
	// errors when the hub already holds a different account's token.
	ClaimHubToken func(name, token string) error
}

// defaultPreparers is the preparer for each type that needs more than
// validation. The empty type (a definition with none) does nothing.
func defaultPreparers() map[string]preparer {
	return map[string]preparer{
		"":           noopPreparer{},
		"duckdb":     duckdbPreparer{},
		"sqlite":     sqlitePreparer{},
		"ducklake":   ducklakePreparer{},
		"motherduck": motherduckPreparer{},
	}
}

// preparerFor returns the preparer for typ; a type without one is only validated.
func (o sessionOpts) preparerFor(typ string) preparer {
	if p, ok := o.preparers[typ]; ok {
		return p
	}
	return validatePreparer{}
}

// validatePreparer is the default: connections.Validate, attached on first use.
type validatePreparer struct{}

func (validatePreparer) ReadOnly(rc ResolvedConnection, forced bool) (bool, error) {
	return rc.Resolved.IsReadOnly() || forced, nil
}

func (validatePreparer) Prepare(_ context.Context, _ prepareEnv, _ ResolvedConnection, def connections.Def, _ *session.Spec) error {
	return connections.Validate(def)
}

func (validatePreparer) Eager() bool { return false }

type noopPreparer struct{ validatePreparer }

func (noopPreparer) Prepare(context.Context, prepareEnv, ResolvedConnection, connections.Def, *session.Spec) error {
	return nil
}

// sqlitePreparer validates like the default and attaches eagerly (a local file).
type sqlitePreparer struct{ validatePreparer }

func (sqlitePreparer) Eager() bool { return true }

// duckdbPreparer attaches a local DuckDB file eagerly. A file that does not
// exist yet is created by the first write, in a folder made only inside the
// workspace; a read-only open reports it instead.
type duckdbPreparer struct{ validatePreparer }

func (duckdbPreparer) Eager() bool { return true }

func (duckdbPreparer) Prepare(_ context.Context, env prepareEnv, rc ResolvedConnection, _ connections.Def, spec *session.Spec) error {
	spec.Path = rc.Path
	if env.SkipLocal {
		return nil
	}
	if env.ForceReadOnly {
		if _, err := os.Stat(rc.Path); err != nil {
			return fmt.Errorf("connection %q has no data file yet; it is created the first time something writes to it", rc.Name)
		}
	}
	if !spec.ReadOnly && insideRoot(env.Root, rc.Path) {
		if err := os.MkdirAll(filepath.Dir(rc.Path), 0o755); err != nil {
			return fmt.Errorf("connection %q: failed to create its folder: %w", rc.Name, err)
		}
	}
	return nil
}

// ducklakePreparer validates, then makes the folders of a new lake, inside the
// workspace only; a path elsewhere must already have its folder. A folder that
// cannot be made is reported without dropping the connection.
type ducklakePreparer struct{ validatePreparer }

func (p ducklakePreparer) Prepare(ctx context.Context, env prepareEnv, rc ResolvedConnection, def connections.Def, spec *session.Spec) error {
	if err := p.validatePreparer.Prepare(ctx, env, rc, def, spec); err != nil {
		return err
	}
	if spec.ReadOnly {
		return nil
	}
	var dirs []string
	if !connections.IsDataURL(rc.Path) { // a URL catalog is read-only (returned above); the guard is belt and braces
		dirs = append(dirs, filepath.Dir(rc.Path))
	}
	if dp := def.Options["data_path"]; dp != "" && !connections.IsDataURL(dp) {
		dirs = append(dirs, dp)
	}
	for _, d := range dirs {
		if !insideRoot(env.Root, d) {
			continue
		}
		if err := os.MkdirAll(d, 0o755); err != nil {
			env.Fail(rc.Name, fmt.Sprintf("connection %q: failed to create its folder: %v", rc.Name, err))
		}
	}
	return nil
}

// motherduckPreparer handles the one hub-wide account: a MotherDuck connection
// cannot be attached read-only (a read-only open leaves it registered but
// unconnectable), and the workspace holds one token.
type motherduckPreparer struct{ validatePreparer }

func (motherduckPreparer) ReadOnly(rc ResolvedConnection, _ bool) (bool, error) {
	if rc.Resolved.ReadOnly != nil && *rc.Resolved.ReadOnly {
		return false, errors.New(connections.ErrMotherDuckReadOnly)
	}
	return false, nil
}

func (p motherduckPreparer) Prepare(ctx context.Context, env prepareEnv, rc ResolvedConnection, def connections.Def, spec *session.Spec) error {
	if env.ForceReadOnly {
		spec.Params["forced_read_only"] = "true"
	}
	if err := p.validatePreparer.Prepare(ctx, env, rc, def, spec); err != nil {
		return err
	}
	if spec.Credential == nil {
		return nil
	}
	// Keychain / env read only; a failure is reported by Preflight.
	cred, err := spec.Credential(ctx)
	if err != nil || cred.Token == "" {
		return nil //nolint:nilerr // reported by Preflight at registration
	}
	return env.ClaimHubToken(rc.Name, cred.Token)
}
