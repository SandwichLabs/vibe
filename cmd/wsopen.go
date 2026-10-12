package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/hub"
	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/workspace"
	"github.com/spf13/cobra"
)

// errLegacyFileArg is what every command that used to take a .vibe file says
// when it is handed one. `vibe mcp serve` keeps hub.ErrLegacyFile.
var errLegacyFileArg = errors.New(".vibe files are no longer opened; create a workspace with `vibe workspace init`")

// isVibeFile reports whether a path argument (a folder, --workspace, a file to
// import) names a legacy .vibe container. It is only for arguments that are
// paths: never SQL, and never a table name (see isVibeTableArg).
func isVibeFile(arg string) bool {
	return strings.EqualFold(filepath.Ext(strings.TrimSpace(arg)), ".vibe")
}

// isVibeTableArg is isVibeFile for an argument that may be a path or a table
// name: `s.vibe` is a legal schema.table, so it only counts as a container
// when it cannot be a table: it has no whitespace or quotes and either names
// an existing file or has a path separator.
func isVibeTableArg(arg string) bool {
	arg = strings.TrimSpace(arg)
	if !isVibeFile(arg) || strings.ContainsAny(arg, " \t\r\n\"'") {
		return false
	}
	if strings.ContainsAny(arg, `/\`) {
		return true
	}
	fi, err := os.Stat(arg)
	return err == nil && fi.Mode().IsRegular()
}

// cliResolver is the credential resolver of CLI commands: OS keychain,
// environment as it was when the command started, secrets scrubbed through
// the CLI redactor.
func cliResolver() *secrets.Resolver {
	return secrets.NewResolver(secrets.WithRedactor(cliRedactor), secrets.WithResolverLogger(logger), secrets.WithEnv(workspace.SnapshotEnv()))
}

// addWorkspaceFlag registers the shared --workspace flag.
func addWorkspaceFlag(c *cobra.Command) {
	c.Flags().String("workspace", "", "workspace folder (default: the nearest folder above the current directory with a vibe.yaml)")
}

// openedWorkspace is a workspace opened for one CLI command: its stores, its
// DuckDB hub, and (unless read-only) the hub lock that makes this process the
// only writer. Close releases them in the reverse order they were taken.
type openedWorkspace struct{ *hub.Opened }

// hubHeldError is the friendly form of a hub.HeldError. It still satisfies
// errors.Is(err, hub.ErrHubHeld), so a caller can offer --read-only.
type hubHeldError struct {
	msg string
	err error
}

func (e *hubHeldError) Error() string { return e.msg }
func (e *hubHeldError) Unwrap() error { return e.err }

// heldMessage turns a hub.HeldError into advice that names the holder. Only a
// command that has a --read-only flag (advise) suggests it, and it says what
// it reaches: the owner keeps the workspace's local DuckDB files open for
// writing, and DuckDB cannot read a file while a writer has it.
func heldMessage(root string, err error, advise bool) error {
	var held *hub.HeldError
	if !errors.As(err, &held) {
		return err
	}
	who := "another Vibe process"
	if held.Holder != nil {
		who = held.Holder.String()
	}
	msg := fmt.Sprintf("workspace %s is open in %s, which owns its database; close it first", filepath.Base(root), who)
	if advise {
		msg += ", or re-run with --read-only (that reads connections other than local DuckDB files, such as MotherDuck or Postgres; the owner keeps the workspace's own .duckdb files locked)"
	}
	return &hubHeldError{msg: msg, err: err}
}

// lockedMessage is the plain-language form of DuckDB's lock error for a local
// connection: who has it, and what that means.
func lockedMessage(root string) string {
	who := "another process"
	if h, err := hub.ReadHolder(root); err == nil && h != nil {
		who = h.String()
	}
	return fmt.Sprintf("a local DuckDB file that %s has open for writing; DuckDB cannot read it while a writer holds it, so close that process first (--read-only only reaches other connections)", who)
}

// cliBeforeSession is what the CLI shows between taking the hub and opening
// the session: the workspace's warnings, then the .env question (its answer
// decides which credentials the session can resolve).
func cliBeforeSession(errW io.Writer) func(workspace.Workspace) {
	return func(ws workspace.Workspace) {
		for _, w := range ws.Warnings() {
			fmt.Fprintln(errW, "warning:", w)
		}
		maybePromptDotEnv(ws, errW)
	}
}

// cliOpen opens the workspace through hub.Open and words its errors for the CLI.
func cliOpen(ctx context.Context, dir string, mode hub.Mode, advise bool, app string, errW io.Writer) (*openedWorkspace, error) {
	if isVibeFile(dir) {
		return nil, errLegacyFileArg
	}
	root, err := hub.ResolveRoot(dir, "")
	if err != nil {
		return nil, err
	}
	o, err := hub.Open(ctx, root, hub.OpenOptions{
		App: app, Mode: mode, Logger: logger, Resolver: cliResolver(), Redactor: cliRedactor,
		BeforeSession: cliBeforeSession(errW),
	})
	if err != nil {
		var oe *hub.OpenError
		if errors.As(err, &oe) {
			switch oe.Step {
			case hub.StepOpen:
				return nil, fmt.Errorf("failed to open the workspace: %w", oe.Err)
			case hub.StepHub:
				return nil, heldMessage(root, oe.Err, advise)
			}
			return nil, oe.Err
		}
		return nil, err
	}
	return &openedWorkspace{o}, nil
}

// openWorkspaceRemoteOnly opens the workspace without the hub lock and without
// attaching its local files, for `vibe conn connect` of a connection that
// cannot be attached read-only (MotherDuck): nothing here writes a file.
func openWorkspaceRemoteOnly(ctx context.Context, dir string, errW io.Writer) (*openedWorkspace, error) {
	return cliOpen(ctx, dir, hub.ModeRemoteOnly, false, "", errW)
}

// openWorkspace finds the workspace (dir, or the nearest one above the working
// directory), opens it and its session. Unless readOnly it first takes the hub
// lock (and says who holds it when it cannot); with readOnly it opens without
// the lock and attaches every connection read-only. advise says whether the
// command has a --read-only flag its "held" error may suggest.
func openWorkspace(ctx context.Context, dir string, readOnly, advise bool, app string, errW io.Writer) (*openedWorkspace, error) {
	mode := hub.ModeReadWrite
	if readOnly {
		mode = hub.ModeReadOnly
	}
	return cliOpen(ctx, dir, mode, advise, app, errW)
}

// hasIssue reports whether a connection failed to come up.
func (o *openedWorkspace) hasIssue(name string) bool {
	_, ok := o.issueFor(name)
	return ok
}

// States is every connection as all surfaces show it (workspace.ConnectionStates).
func (o *openedWorkspace) States() []workspace.ConnectionState {
	return workspace.ConnectionStates(o.WS, o.Sess, o.Issues)
}

// isLocked reports whether the connection is held by another process, from its
// typed status (never from a message).
func (o *openedWorkspace) isLocked(name string) bool {
	for _, st := range o.States() {
		if st.Name == name {
			return st.Locked()
		}
	}
	return false
}

// issueFor returns the session issue recorded for a connection, if any.
func (o *openedWorkspace) issueFor(name string) (workspace.SessionIssue, bool) {
	for _, i := range o.Issues {
		if i.Connection == name {
			return i, true
		}
	}
	return workspace.SessionIssue{}, false
}

// requireConnection fails with the connection's own problem when the command
// needs a connection that did not come up, and warns about the rest.
func (o *openedWorkspace) requireConnection(name string, errW io.Writer) error {
	for _, i := range o.Issues {
		if i.Connection == name {
			continue
		}
		fmt.Fprintf(errW, "warning: connection %s is not available: %s\n", i.Connection, i.Message)
	}
	if name == "" {
		return nil
	}
	if i, ok := o.issueFor(name); ok {
		if o.isLocked(name) {
			return fmt.Errorf("connection %s is %s", name, lockedMessage(o.WS.Root()))
		}
		return fmt.Errorf("connection %s is not available: %s", name, i.Message)
	}
	return nil
}

// knownConnection reports whether name is one of the workspace's merged
// connections.
func (o *openedWorkspace) knownConnection(name string) (workspace.Connection, bool) {
	return o.WS.Connection(name)
}

// resolveTarget parses "table", "schema.table", "conn.table" or
// "conn.schema.table". A two-part name whose first part is a workspace
// connection means that connection (as the plan's `<conn>.<table>`); an
// unqualified name means the default connection. The connection is checked
// (requireConnection) before the table is looked up, so a connection that is
// down is reported as that, not as a missing table. The result is fully
// qualified through the session.
func (o *openedWorkspace) resolveTarget(ctx context.Context, name string, errW io.Writer) (session.TableRef, error) {
	ref, err := engine.ParseTable(name)
	if err != nil {
		return session.TableRef{}, err
	}
	if ref.Catalog == "" && ref.Schema != "" {
		if _, ok := o.knownConnection(ref.Schema); ok {
			ref.Catalog, ref.Schema = ref.Schema, ""
		}
	}
	if ref.Catalog == "" {
		ref.Catalog = o.WS.DefaultConnection()
	}
	if err := o.requireConnection(ref.Catalog, errW); err != nil {
		return session.TableRef{}, err
	}
	return o.Sess.Resolve(ctx, ref)
}
