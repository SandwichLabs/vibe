package workspace

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sandwich-labs/vibe-app/internal/connections"
	"github.com/sandwich-labs/vibe-app/internal/secrets"
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
	skipLocal bool
	resolver  *secrets.Resolver
	lookup    LookupFunc
	session   []session.Option
	readOnly  bool
}

// WithForceReadOnly attaches every connection read-only whatever its
// definition says, for a process that does not own the hub (`vibe query
// --read-only`, `vibe workspace info`). A local DuckDB file that does not
// exist yet is reported as an issue instead of being created, and no folder
// is created for it.
func WithForceReadOnly() SessionOption { return func(o *sessionOpts) { o.readOnly = true } }

// WithSkipLocalFiles registers local duckdb and sqlite connections without
// attaching them (nor creating their files), for a command that only needs the
// remote ones and does not own the hub (`vibe conn connect`).
func WithSkipLocalFiles() SessionOption { return func(o *sessionOpts) { o.skipLocal = true } }

// WithSessionResolver replaces the workspace's credential resolver for this
// session (default: ws.Resolver()).
func WithSessionResolver(r *secrets.Resolver) SessionOption {
	return func(o *sessionOpts) { o.resolver = r }
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
//   - Local `duckdb` and `sqlite` file connections are attached eagerly (design
//     #106 item 8: a local file, nothing to dial). Every other type is only
//     registered and preflighted (credential and extension state, no network);
//     it connects on first use. The one MotherDuck token the hub may hold is
//     resolved here and handed to session.New, before the configuration locks.
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
	resolver := o.resolver
	if resolver == nil {
		resolver = ws.Resolver()
	}

	conns := ws.Connections()
	def := ws.DefaultConnection()
	sort.SliceStable(conns, func(i, j int) bool { // default first
		return conns[i].Name == def && conns[j].Name != def
	})

	var issues []SessionIssue
	failMsg := func(name, msg string) {
		logger.Warn("connection not available", "name", name, "reason", msg)
		issues = append(issues, SessionIssue{Connection: name, Message: msg})
	}

	// Pass 1: resolve every definition (no network, no dialling). The one
	// MotherDuck token the hub opens with is resolved here, because it must be
	// SET before the configuration is locked (spike #107).
	type item struct {
		rc   ResolvedConnection
		spec session.Spec
	}
	var items []item
	mdToken, mdOwner := "", ""
	for _, c := range conns {
		rc, err := ResolveConnection(ws, c, o.lookup)
		if err != nil {
			failMsg(c.Name, redactBasic(err.Error()))
			continue
		}
		resolved := rc.Resolved
		readOnly := resolved.IsReadOnly() || o.readOnly
		if resolved.Type == "motherduck" && resolved.ReadOnly != nil && *resolved.ReadOnly {
			failMsg(c.Name, connections.ErrMotherDuckReadOnly)
			continue
		}
		if resolved.Type == "motherduck" {
			// Cannot be attached read-only: a read-only command leaves it
			// registered but unconnectable (status shows why on Connect).
			readOnly = false
		}
		cdef := rc.Def(readOnly)
		spec := cdef.Spec()
		if resolved.Type == "motherduck" && o.readOnly {
			spec.Params["forced_read_only"] = "true"
		}
		if resolved.Type == "duckdb" {
			spec.Path = rc.Path
			if o.skipLocal {
				items = append(items, item{rc: rc, spec: spec})
				continue
			}
			if o.readOnly {
				if _, err := os.Stat(rc.Path); err != nil {
					failMsg(c.Name, fmt.Sprintf("connection %q has no data file yet; it is created the first time something writes to it", c.Name))
					continue
				}
			}
			// Only ever create folders inside the workspace; a path the user
			// pointed elsewhere must already have its folder.
			if !spec.ReadOnly && insideRoot(ws.Root(), rc.Path) {
				if err := os.MkdirAll(filepath.Dir(rc.Path), 0o755); err != nil {
					failMsg(c.Name, fmt.Sprintf("connection %q: failed to create its folder: %v", c.Name, err))
					continue
				}
			}
		} else if resolved.Type != "" {
			if err := connections.Validate(cdef); err != nil {
				failMsg(c.Name, redactBasic(err.Error()))
				continue
			}
		}
		if connections.NeedsCredential(resolved.Type) {
			ref := credentialRef(ws.ID(), c.Scope, c.Name)
			b, method := rc.Binding(), c.SignIn
			guard := signInGuard(c, rc.Resolved)
			spec.Credential = func(ctx context.Context) (secrets.Credential, error) {
				if guard != nil {
					return secrets.Credential{}, guard
				}
				return resolver.Resolve(ctx, ref, method, b)
			}
			if resolved.Type == "motherduck" {
				// Keychain / env read only; a failure is reported by Preflight.
				if cred, err := spec.Credential(ctx); err == nil && cred.Token != "" {
					switch {
					case mdToken == "":
						mdToken, mdOwner = cred.Token, c.Name
					case cred.Token != mdToken:
						failMsg(c.Name, fmt.Sprintf("this workspace already uses the MotherDuck account of %q, and one workspace uses one account; sign this connection in with the same account or remove it", mdOwner))
						continue
					}
				}
			}
		}
		items = append(items, item{rc: rc, spec: spec})
	}

	sopts := []session.Option{
		session.WithLogger(logger),
		session.WithConnectionTypes(connections.Types()...),
		session.WithRedactor(resolver.Redactor()),
	}
	if mdToken != "" {
		sopts = append(sopts, session.WithMotherDuckToken(mdToken))
	}
	sess, err := session.New(append(sopts, o.session...)...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open the workspace session: %w", err)
	}
	fail := func(name string, err error) { failMsg(name, sess.Redact(err.Error())) }

	// Pass 2: register everything; attach local files eagerly; leave the rest
	// to connect on first use (lazy attach).
	attached := map[string]bool{}
	for _, it := range items {
		name := it.rc.Name
		if err := sess.Register(it.spec); err != nil {
			fail(name, err)
			continue
		}
		switch it.rc.Resolved.Type {
		case "duckdb", "sqlite":
			if o.skipLocal {
				continue
			}
			if err := sess.Connect(ctx, name); err != nil {
				fail(name, err)
				continue
			}
			attached[name] = true
		default:
			sess.Preflight(ctx, name)
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

// redactBasic scrubs credential-shaped text from a message produced before a
// session (and its redactor) exists.
func redactBasic(s string) string { return secrets.RedactPatterns(s) }
