package secrets

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrNotFound means the store holds nothing for the reference.
	ErrNotFound = errors.New("secret not found")
	// ErrUnavailable means the backing store cannot be used (no keychain, a
	// locked or unresponsive one). It is never "not found".
	ErrUnavailable = errors.New("secret store unavailable")
	// ErrReadOnly is returned when writing to the environment store.
	ErrReadOnly = errors.New("secret store is read-only")
)

// Store keeps opaque secret values by reference. Implementations must be safe
// for concurrent use. Delete of a missing value is not an error.
type Store interface {
	Get(ctx context.Context, ref Ref) ([]byte, error)
	Set(ctx context.Context, ref Ref, value []byte) error
	Delete(ctx context.Context, ref Ref) error
}

// Reference scopes.
const (
	ScopeWorkspace = "workspace"
	ScopeUser      = "user"
	ScopeApp       = "app"
)

// Ref names one stored value. Workspace connections are keyed by workspace id
// and connection name, user connections by name alone, application secrets
// (the LLM key) by name under the app scope.
type Ref struct {
	Scope string
	Owner string
	Name  string
}

// WorkspaceRef is the reference of a workspace connection's credential.
func WorkspaceRef(workspaceID, connection string) Ref {
	return Ref{Scope: ScopeWorkspace, Owner: workspaceID, Name: connection}
}

// UserRef is the reference of a user-scope connection's credential.
func UserRef(connection string) Ref { return Ref{Scope: ScopeUser, Name: connection} }

// AppRef is the reference of an application secret such as "llm-anthropic".
func AppRef(name string) Ref { return Ref{Scope: ScopeApp, Name: name} }

// Validate rejects a reference that cannot be keyed unambiguously.
func (r Ref) Validate() error {
	switch r.Scope {
	case ScopeWorkspace:
		if r.Owner == "" {
			return errors.New("secret reference: workspace scope needs a workspace id")
		}
	case ScopeUser, ScopeApp:
		if r.Owner != "" {
			return fmt.Errorf("secret reference: %s scope takes no owner", r.Scope)
		}
	default:
		return fmt.Errorf("secret reference: unknown scope %q", r.Scope)
	}
	if r.Name == "" {
		return errors.New("secret reference: empty name")
	}
	for _, part := range []string{r.Owner, r.Name} {
		if strings.ContainsAny(part, "/\x00") {
			return errors.New("secret reference: names cannot contain '/'")
		}
	}
	return nil
}

// Account is the stable key used by keyring-style backends.
func (r Ref) Account() string {
	return r.Scope + "/" + r.Owner + "/" + r.Name
}

// String is safe to log: a reference names a place, never a value.
func (r Ref) String() string { return r.Account() }
