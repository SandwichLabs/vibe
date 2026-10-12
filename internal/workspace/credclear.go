package workspace

import (
	"context"
	"errors"

	"github.com/SandwichLabs/vibe/internal/secrets"
)

// ClearCredential removes a connection's saved credential: one field of it
// when field is set (user | password | token | key_id | secret), the whole
// credential when it is empty. A field that holds nothing says so in plain
// words instead of the keychain's "not found".
func ClearCredential(ctx context.Context, ws Workspace, name, field string) error {
	if field == "" {
		return ws.ClearCredentials(ctx, name)
	}
	c, ok := ws.Connection(name)
	if !ok {
		return &UnknownConnectionError{Name: name}
	}
	err := ws.Resolver().ClearCredentialField(ctx, CredentialRef(ws.ID(), c.Scope, c.Name), c.SignIn, field)
	if errors.Is(err, secrets.ErrNotFound) {
		return errors.New("There is nothing saved to clear for this connection.")
	}
	return err
}
