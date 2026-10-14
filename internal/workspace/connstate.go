package workspace

import (
	"github.com/SandwichLabs/vibe/internal/connections"
	"github.com/SandwichLabs/vibe/internal/session"
)

// ConnectionState is one connection as every surface shows it: Studio's rail,
// `vibe conn list` and `vibe workspace info`. It is built from typed fields (the
// session's status, the open issues), never by reading a message back, so the
// three cannot disagree on a status word or detect a lock or a changed server
// differently.
type ConnectionState struct {
	Name    string
	Type    string
	Scope   Scope
	Shadows []Scope
	Default bool
	SignIn  string
	// ReadOnly is the definition's flag; EffectiveReadOnly is what the attach
	// actually is (a read-only open forces every connection read-only).
	ReadOnly          bool
	EffectiveReadOnly bool
	// Status is one of the session.Status* words, the same on every surface.
	Status session.StatusKind
	// Detail is one plain line for the status ("" when ok). For an error it is
	// the plain-language classification when there is one, else the redacted
	// message.
	Detail string
	// LockedByPID is the process holding a locked-by connection's file (0 when
	// unknown).
	LockedByPID int
	// ServerChanged is set on needs-credentials that is because the definition
	// now points at a different server than the saved sign-in.
	ServerChanged *session.ServerChange
	// Extension names the extension an extension-missing connection needs.
	Extension string
	// Remote connections dial on first use; Catalogless ones (s3, gcs, r2)
	// have no tables.
	Remote      bool
	Catalogless bool
}

// ConnectionStates merges ws's connections with sess's statuses and the
// issues OpenSession reported. A connection with no session status is
// not-connected, or error when its definition could not even be registered
// (a missing ${ENV}, a path outside the folder, a file in use).
func ConnectionStates(ws Workspace, sess session.Session, issues []SessionIssue) []ConnectionState {
	status := map[string]session.Status{}
	infos := map[string]session.ConnectionInfo{}
	if sess != nil {
		for _, s := range sess.Connections() {
			status[s.Name] = s
		}
		for _, i := range sess.ConnectionInfos() {
			infos[i.Name] = i
		}
	}
	issue := map[string]string{}
	for _, i := range issues {
		issue[i.Connection] = i.Message
	}
	conns := ws.Connections()
	out := make([]ConnectionState, 0, len(conns))
	for _, c := range conns {
		st := ConnectionState{
			Name: c.Name, Type: c.Type, Scope: c.Scope, Shadows: c.Shadows, Default: c.Default, SignIn: c.SignIn,
			ReadOnly: c.IsReadOnly(), EffectiveReadOnly: c.IsReadOnly(),
			Status: session.StatusNotConnected, Remote: connections.IsRemote(c.Type),
		}
		if i, ok := infos[c.Name]; ok {
			st.Remote, st.Catalogless = i.Remote, i.Catalogless
		}
		s, registered := status[c.Name]
		if registered {
			st.Status, st.Detail, st.LockedByPID, st.EffectiveReadOnly = s.Kind, s.Message, s.LockedByPID, s.ReadOnly
		}
		if msg, ok := issue[c.Name]; ok {
			if !registered {
				st.Status, st.Detail = session.StatusError, msg
			} else if st.Detail == "" {
				st.Detail = msg
			}
		}
		switch st.Status {
		case session.StatusError:
			// One plain line; the raw (redacted) text is in the editor's Test.
			if st.Detail != "" {
				if code, msg := connections.Classify(connections.Failure{Raw: st.Detail, Def: connections.Def{Type: c.Type, Host: c.Host, Database: c.Database}}); code != connections.CodeError {
					st.Detail = msg
				}
			}
		case session.StatusExtensionMissing:
			st.Extension = s.Extension
			if ti, err := connections.TypeInfoFor(c.Type); err == nil && st.Extension == "" && len(ti.Extensions) > 0 {
				st.Extension = ti.Extensions[0]
			}
		case session.StatusNeedsCredentials:
			st.ServerChanged = s.ServerChanged
		}
		out = append(out, st)
	}
	return out
}

// Locked reports whether the connection is held by another process.
func (s ConnectionState) Locked() bool { return s.Status == session.StatusLockedBy }
