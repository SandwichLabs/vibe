package main

// Testing a definition without saving it, and the MotherDuck database picker.
// (Named probe, not test: a _test.go suffix would make it a test file.)

import (
	"context"
	"errors"
	"strings"

	"github.com/SandwichLabs/vibe/internal/connections"
	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

// TestConnectionRequest tests a definition without saving it. Credentials are
// what the person just typed (new, never returned); fields left empty fall back
// to the saved credential of an existing connection of the same name, but only
// while the definition still points at the server it was saved for.
type TestConnectionRequest struct {
	Scope       string                  `json:"scope"`
	Def         workspace.ConnectionDef `json:"def"`
	SignIn      string                  `json:"signIn"`
	Credentials CredentialInput         `json:"credentials"`
}

// TestError is a failed test in plain language, plus the redacted raw text.
type TestError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details string `json:"details"`
}

// TestResultView is what TestConnection returns. Nothing in it is a secret.
type TestResultView struct {
	OK      bool       `json:"ok"`
	Status  string     `json:"status"`
	Ms      int64      `json:"ms"`
	Count   int        `json:"count"`
	Detail  string     `json:"detail,omitempty"`
	Summary string     `json:"summary,omitempty"`
	Error   *TestError `json:"error,omitempty"`
	// InstallExtension names an extension to install (then test again).
	InstallExtension string `json:"installExtension,omitempty"`
}

// TestConnection attaches the definition in a throwaway session and probes it,
// with a 10 second limit. It never touches the workspace's own session.
func (a *App) TestConnection(req TestConnectionRequest) (*TestResultView, error) {
	ws, _, _, err := a.snapshot()
	if err != nil {
		return nil, err
	}
	scope := workspace.ScopeLocal
	if req.Scope != "" {
		if scope, err = scopeChoice(req.Scope); err != nil {
			return nil, err
		}
	}
	def := req.Def
	if def.Name == "" {
		def.Name = "test"
	}
	typed := req.Credentials.toCredential()
	a.secrets.Redactor().AddCredential(typed)
	return a.probe(ws, workspace.Connection{ConnectionDef: def, Scope: scope}, req.SignIn, typed), nil
}

// probe resolves c, finds the credential to test it with and runs the test. Every
// failure is a TestResultView (redacted), not an error.
func (a *App) probe(ws workspace.Workspace, c workspace.Connection, signIn string, typed secrets.Credential) *TestResultView {
	fail := func(code, msg, details string) *TestResultView {
		return &TestResultView{Status: string(session.StatusError), Error: &TestError{Code: code, Message: msg, Details: a.redact(details)}}
	}
	rc, err := workspace.ResolveConnection(ws, c, nil)
	if err != nil {
		return fail(connections.CodeInvalid, a.redact(err.Error()), err.Error())
	}
	cred, err := workspace.TestCredential(a.ctxOrBackground(), ws, rc, signIn, typed)
	var changed *secrets.ErrFingerprintChanged
	if errors.As(err, &changed) {
		return fail(connections.CodeServerChanged, connections.ServerChangedMessage, "")
	}
	a.secrets.Redactor().AddCredential(cred)
	// The test never reads a read-only flag a person might have off.
	res := connections.Test(a.ctxOrBackground(), rc.Def(true), cred, connections.WithTestRedactor(a.secrets.Redactor()))
	out := &TestResultView{OK: res.OK, Status: string(res.Status), Ms: res.Latency.Milliseconds(), Count: res.Count, Detail: res.Detail, InstallExtension: res.InstallExtension}
	if res.OK {
		out.Summary = a.redact(res.Detail)
		return out
	}
	out.Error = &TestError{Code: res.Code, Message: a.redact(res.Message), Details: a.redact(res.Error)}
	return out
}

// ListMotherDuckDatabasesRequest asks which databases a MotherDuck token can
// attach. Credentials are what the person just typed (write-only: never
// returned); with none typed, the sign-in method in effect supplies the token
// (keychain, or an env: variable), like Test.
type ListMotherDuckDatabasesRequest struct {
	// Name is the connection being edited ("" for a new one).
	Name        string          `json:"name"`
	Scope       string          `json:"scope"`
	SignIn      string          `json:"signIn"`
	Credentials CredentialInput `json:"credentials"`
	// Install is the person's consent to download the MotherDuck extension.
	Install bool `json:"install"`
}

// MotherDuckDatabase is one entry of the database picker.
type MotherDuckDatabase struct {
	Name  string `json:"name"`
	Share bool   `json:"share"`
}

// MotherDuckDatabasesView is what ListMotherDuckDatabases returns. Nothing in
// it is a secret.
type MotherDuckDatabasesView struct {
	OK        bool                 `json:"ok"`
	Status    string               `json:"status"`
	Databases []MotherDuckDatabase `json:"databases"`
	// Source says where the token came from ("typed", "keychain", "env"),
	// never the token itself.
	Source string `json:"source,omitempty"`
	// Message is a redacted plain-language reason when OK is false.
	Message          string `json:"message,omitempty"`
	InstallExtension string `json:"installExtension,omitempty"`
}

// ListMotherDuckDatabases lists the MotherDuck databases and shares the token
// can attach, in a subprocess (a process holds one MotherDuck token), with the
// same 10 second limit as Test. It never touches the workspace's own session.
func (a *App) ListMotherDuckDatabases(req ListMotherDuckDatabasesRequest) (*MotherDuckDatabasesView, error) {
	ws, _, _, err := a.snapshot()
	if err != nil {
		return nil, err
	}
	empty := func(status, msg string) *MotherDuckDatabasesView {
		return &MotherDuckDatabasesView{Status: status, Databases: []MotherDuckDatabase{}, Message: a.redact(msg)}
	}
	typed := req.Credentials.toCredential()
	if strings.HasPrefix(req.SignIn, "env:") {
		typed = secrets.Credential{} // an environment sign-in never uses a typed token
	}
	a.secrets.Redactor().AddCredential(typed)
	cred, source := secrets.Credential{Token: typed.Token}, "typed"
	if typed.Token == "" {
		var ok bool
		if cred, source, ok = a.savedMotherDuckToken(ws, req); !ok {
			return empty(string(session.StatusNeedsCredentials), "Enter a MotherDuck token, or choose an environment variable that holds one, to load your databases."), nil
		}
	}
	a.secrets.Redactor().AddCredential(cred)
	res := connections.ListMotherDuckDatabases(a.ctxOrBackground(), cred, connections.WithInstall(req.Install), connections.WithTestRedactor(a.secrets.Redactor()))
	out := &MotherDuckDatabasesView{OK: res.OK, Status: string(res.Status), Databases: []MotherDuckDatabase{}, Source: source, InstallExtension: res.InstallExtension}
	for _, d := range res.Databases {
		out.Databases = append(out.Databases, MotherDuckDatabase{Name: d.Name, Share: d.Share})
	}
	if !res.OK {
		out.Message = a.redact(listFailureMessage(res))
	}
	return out, nil
}

func listFailureMessage(res connections.MDDatabasesResult) string {
	switch {
	case res.InstallExtension != "" || res.Status == session.StatusExtensionMissing:
		return "Vibe's database engine needs the motherduck extension, which isn't installed yet. Install it, then load again."
	case res.Status == session.StatusNeedsCredentials:
		return "MotherDuck turned down this token. Check it and try again."
	case res.TimedOut:
		return "No answer from MotherDuck after 10 seconds. Check your network connection."
	}
	return "Couldn't load your MotherDuck databases. You can still type the database name. " + res.Error
}

// savedMotherDuckToken is the token the sign-in method in effect supplies when
// none was typed: the keychain or prompt entry of the connection being edited,
// or an env: variable. It is looked up against the saved definition, not the
// form (the Database field is what this call is for choosing).
func (a *App) savedMotherDuckToken(ws workspace.Workspace, req ListMotherDuckDatabasesRequest) (secrets.Credential, string, bool) {
	method := req.SignIn
	var saved *workspace.Connection
	if c, ok := ws.Connection(req.Name); ok && req.Name != "" {
		saved = &c
		if method == "" {
			method = c.SignIn
		}
	}
	if method == "" {
		method = "keychain"
	}
	kind, _, perr := secrets.ParseMethod(method)
	if perr != nil {
		return secrets.Credential{}, "", false
	}
	ctx, cancel := context.WithTimeout(a.ctxOrBackground(), keychainTimeout)
	defer cancel()
	switch {
	case kind == "env":
		ref := secrets.Ref{Scope: secrets.ScopeWorkspace, Name: "motherduck"}
		cred, err := ws.Resolver().Resolve(ctx, ref, method, secrets.Binding{Type: "motherduck"})
		if err != nil || cred.Token == "" {
			return secrets.Credential{}, "", false
		}
		return cred, "env", true
	case saved != nil && saved.Type == "motherduck":
		rc, err := workspace.ResolveConnection(ws, *saved, nil)
		if err != nil {
			return secrets.Credential{}, "", false
		}
		cred, err := ws.Resolver().Resolve(ctx, workspace.CredentialRef(ws.ID(), rc.Scope, saved.Name), method, rc.Binding())
		if err != nil || cred.Token == "" {
			return secrets.Credential{}, "", false
		}
		return cred, kind, true
	}
	return secrets.Credential{}, "", false
}
