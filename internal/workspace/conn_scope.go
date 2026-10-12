package workspace

import "fmt"

// DefinitionIn reads name's definition out of one scope's file: the shared
// vibe.yaml, this computer's local settings, or the user file. It is how a
// shadowed definition (the Team copy behind a local override) is read; the
// winning one is Workspace.Connection.
func DefinitionIn(ws Workspace, scope Scope, name string) (ConnectionDef, bool) {
	var defs []ConnectionDef
	switch scope {
	case ScopeShared:
		defs = ws.Config().Connections
	case ScopeLocal:
		defs = ws.Local().Connections
	case ScopeUser:
		if u, err := LoadUserConfig(); err == nil {
			defs = u.Connections
		}
	}
	for _, d := range defs {
		if d.Name == name {
			return d, true
		}
	}
	return ConnectionDef{}, false
}

// DefinitionView is one connection's definition as the editor shows it: the
// winning one, or (when a scope other than the winner's is asked for) its
// shadowed copy in that scope.
type DefinitionView struct {
	Name string
	// Scope is the scope the Def was read from.
	Scope Scope
	Def   ConnectionDef
	// Fingerprint is Fingerprint(Def): what a saved sign-in is bound to.
	Fingerprint string
	// Shadows are the lower scopes that also define the name (none for a
	// shadowed copy).
	Shadows []Scope
	// ShadowedBy is set for a shadowed copy: the winning scope that overrides it.
	ShadowedBy Scope
	// SignIn and SignInHint are the winning definition's.
	SignIn, SignInHint string
}

// UnknownConnectionError says the workspace has no connection called Name.
type UnknownConnectionError struct{ Name string }

func (e *UnknownConnectionError) Error() string {
	return fmt.Sprintf("There is no connection called %q.", e.Name)
}

// InvalidScopeError says the scope string is not shared | local | user.
type InvalidScopeError struct{ Scope string }

func (e *InvalidScopeError) Error() string { return fmt.Sprintf("unknown scope %q", e.Scope) }

// NoScopeCopyError says Name has no definition in Scope.
type NoScopeCopyError struct {
	Name  string
	Scope string
}

func (e *NoScopeCopyError) Error() string {
	return fmt.Sprintf("there is no %s copy of %q", e.Scope, e.Name)
}

// ViewDefinition is the winning definition of name, or its copy in scope when
// scope is set and differs from the winner's (the shadowed Team copy behind
// "Look at the Team one"). The scope is validated only when it differs from
// the winner's. Nothing in the result is a secret.
func ViewDefinition(ws Workspace, name, scope string) (DefinitionView, error) {
	win, ok := ws.Connection(name)
	if !ok {
		return DefinitionView{}, &UnknownConnectionError{Name: name}
	}
	v := DefinitionView{
		Name: win.Name, Scope: win.Scope, Def: win.ConnectionDef,
		Shadows: win.Shadows, SignIn: win.SignIn, SignInHint: win.SignInHint,
	}
	if scope != "" && scope != string(win.Scope) {
		sc, ok := ParseScope(scope)
		if !ok {
			return DefinitionView{}, &InvalidScopeError{Scope: scope}
		}
		def, ok := DefinitionIn(ws, sc, name)
		if !ok {
			return DefinitionView{}, &NoScopeCopyError{Name: name, Scope: scope}
		}
		v.Scope, v.Def, v.ShadowedBy = sc, def, win.Scope
		v.Shadows = nil
	}
	v.Fingerprint = Fingerprint(v.Def)
	return v, nil
}
