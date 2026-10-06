package workspace

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sandwich-labs/vibe-app/internal/session"
)

// SessionIssue is a connection that could not be registered or attached. The
// message is redacted and never carries a credential, a DSN or an environment
// value.
type SessionIssue struct {
	Connection string `json:"connection"`
	Message    string `json:"message"`
}

// SessionOption configures OpenSession.
type SessionOption func(*sessionOpts)

type sessionOpts struct {
	lookup  LookupFunc
	session []session.Option
}

// WithSessionEnv replaces the environment lookup for ${ENV} interpolation.
func WithSessionEnv(f LookupFunc) SessionOption { return func(o *sessionOpts) { o.lookup = f } }

// WithSessionOptions passes options through to session.New.
func WithSessionOptions(opts ...session.Option) SessionOption {
	return func(o *sessionOpts) { o.session = append(o.session, opts...) }
}

// OpenSession builds the workspace's DuckDB hub: session.New() plus every
// merged connection registered.
//
//   - Local `duckdb` file connections are attached eagerly (design #106 item 8:
//     it is a local file, nothing to dial). Every other type is only
//     registered; it connects on first use.
//   - Relative paths resolve against the workspace root and may not leave it,
//     lexically or through a symlink. An absolute path is accepted only from
//     your own scopes (local, user), never from the shared vibe.yaml, and a
//     folder is only ever created inside the workspace. A relative path in a
//     user-scope connection (which belongs to no workspace) is refused.
//   - ${ENV} references are expanded here, and only in your own scopes; a
//     missing variable fails that one connection (reported as an issue), never
//     the whole workspace.
//   - default_connection becomes the session's default catalog once attached.
//
// A connection that cannot be set up is returned as a SessionIssue and left
// out; the workspace still opens with the rest. The error return is for
// failures of the hub itself.
func OpenSession(ctx context.Context, ws Workspace, logger *slog.Logger, opts ...SessionOption) (session.Session, []SessionIssue, error) {
	var o sessionOpts
	for _, f := range opts {
		f(&o)
	}
	if logger == nil {
		logger = slog.Default()
	}
	sess, err := session.New(append([]session.Option{session.WithLogger(logger)}, o.session...)...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open the workspace session: %w", err)
	}

	conns := ws.Connections()
	def := ws.DefaultConnection()
	sort.SliceStable(conns, func(i, j int) bool { // default first
		return conns[i].Name == def && conns[j].Name != def
	})

	var issues []SessionIssue
	fail := func(name string, err error) {
		msg := session.Redact(err.Error())
		logger.Warn("connection not available", "name", name, "reason", msg)
		issues = append(issues, SessionIssue{Connection: name, Message: msg})
	}
	attached := map[string]bool{}
	for _, c := range conns {
		resolved, err := c.ConnectionDef.Resolve(o.lookup, c.Scope)
		if err != nil {
			fail(c.Name, err)
			continue
		}
		spec := session.Spec{Name: resolved.Name, Type: resolved.Type, ReadOnly: resolved.IsReadOnly()}
		if resolved.Type == "duckdb" {
			p, err := ResolveLocalPath(ws.Root(), resolved.Path, c.Scope)
			if err != nil {
				fail(c.Name, fmt.Errorf("connection %q: %w", c.Name, err))
				continue
			}
			spec.Path = p
			// Only ever create folders inside the workspace; a path the user
			// pointed elsewhere must already have its folder.
			if !spec.ReadOnly && insideRoot(ws.Root(), p) {
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					fail(c.Name, fmt.Errorf("connection %q: failed to create its folder: %w", c.Name, err))
					continue
				}
			}
		}
		if err := sess.Register(spec); err != nil {
			fail(c.Name, err)
			continue
		}
		if resolved.Type == "duckdb" {
			if err := sess.Connect(ctx, c.Name); err != nil {
				fail(c.Name, err)
				continue
			}
			attached[c.Name] = true
		}
	}

	if def != "" {
		switch {
		case attached[def]:
			if err := sess.SetDefaultCatalog(ctx, def); err != nil {
				fail(def, err)
			}
		default:
			if !hasIssue(issues, def) {
				fail(def, fmt.Errorf("default connection %q is not attached, so unqualified table names use the in-memory catalog", def))
			}
		}
	}
	return sess, issues, nil
}

func hasIssue(issues []SessionIssue, name string) bool {
	for _, i := range issues {
		if i.Connection == name {
			return true
		}
	}
	return false
}

// insideRoot reports whether the resolved path p lies within the workspace.
func insideRoot(root, p string) bool {
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	return isWithin(real, filepath.Clean(p))
}

// ResolveLocalPath turns a duckdb connection's path into the file to attach.
// A relative path is resolved against root and must stay inside it, including
// through symlinks. An absolute path is accepted only from the scopes that
// belong to one person (local and user): the shared vibe.yaml is written by
// someone else, so an absolute path there is refused (review #109, D-C).
// scope is the scope the definition came from.
func ResolveLocalPath(root, p string, scope Scope) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", fmt.Errorf("a duckdb connection needs a path")
	}
	if isAbsLike(p) {
		if scope == ScopeShared {
			return "", fmt.Errorf("path %q is absolute, which is not allowed in vibe.yaml; use a path inside the workspace", p)
		}
		if !filepath.IsAbs(p) {
			return "", fmt.Errorf("path %q is not a usable absolute path on this system", p)
		}
		return filepath.Clean(p), nil
	}
	if scope == ScopeUser {
		return "", fmt.Errorf("path %q is relative, but this connection belongs to no workspace: use an absolute path", p)
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("failed to resolve the workspace folder: %w", err)
	}
	abs := filepath.Join(realRoot, filepath.FromSlash(p))
	if !isWithin(realRoot, abs) {
		return "", fmt.Errorf("path %q leaves the workspace folder; use an absolute path if that is intended", p)
	}
	// Resolve the deepest existing ancestor (the file itself when present) so a
	// symlinked directory or file cannot point the attach outside the folder.
	existing := abs
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			break
		}
		existing = parent
	}
	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", fmt.Errorf("failed to resolve path %q: %w", p, err)
	}
	if !isWithin(realRoot, resolved) {
		return "", fmt.Errorf("path %q resolves outside the workspace folder through a symbolic link; use an absolute path if that is intended", p)
	}
	tail, _ := filepath.Rel(existing, abs)
	if tail == "." {
		return resolved, nil
	}
	return filepath.Join(resolved, tail), nil
}

func isWithin(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
