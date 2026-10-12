package secrets

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
)

// Credential is the per-connection blob: the person's own fields plus the
// binding to the server they were entered for. Username and KeyID are not
// secrets (they are readable in the UI); Password, Token and Secret are.
type Credential struct {
	User     string `json:"user,omitempty"`
	Password string `json:"password,omitempty"`
	Token    string `json:"token,omitempty"`
	KeyID    string `json:"key_id,omitempty"`
	Secret   string `json:"secret,omitempty"`

	// Fingerprint is workspace.Fingerprint of the definition the credential was
	// entered for; Server is a host-level description of the same (for the
	// "server changed" message). Neither is secret.
	Fingerprint string `json:"fingerprint,omitempty"`
	Server      string `json:"server,omitempty"`
}

// SecretValues lists the values that must never be shown or logged.
func (c Credential) SecretValues() []string {
	var out []string
	for _, v := range []string{c.Password, c.Token, c.Secret} {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

// HasSecret reports whether any secret field is set.
func (c Credential) HasSecret() bool { return len(c.SecretValues()) > 0 }

// Overlay returns c with every field set in typed replacing the same field of
// c: what a person typed in this edit wins over what was stored. The binding
// (Fingerprint, Server) stays c's own; typed values are never bound to a server.
func (c Credential) Overlay(typed Credential) Credential {
	if typed.User != "" {
		c.User = typed.User
	}
	if typed.KeyID != "" {
		c.KeyID = typed.KeyID
	}
	if typed.Password != "" {
		c.Password = typed.Password
	}
	if typed.Token != "" {
		c.Token = typed.Token
	}
	if typed.Secret != "" {
		c.Secret = typed.Secret
	}
	return c
}

// Marshal encodes the blob for a Store.
func (c Credential) Marshal() ([]byte, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("failed to encode credential: %w", err)
	}
	return b, nil
}

// ParseCredential decodes a stored blob. The error never quotes the input.
func ParseCredential(b []byte) (Credential, error) {
	var c Credential
	if err := json.Unmarshal(b, &c); err != nil {
		return Credential{}, errors.New("the stored credential is unreadable: clear it and sign in again")
	}
	return c, nil
}

// String and GoString make an accidental %v / %+v / %#v print safe.
func (c Credential) String() string {
	return fmt.Sprintf("Credential{user:%q key_id:%q secret_set:%t}", c.User, c.KeyID, c.HasSecret())
}

// GoString implements fmt.GoStringer.
func (c Credential) GoString() string { return c.String() }

// LogValue implements slog.LogValuer: the readable fields only.
func (c Credential) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("user", c.User),
		slog.String("key_id", c.KeyID),
		slog.Bool("secret_set", c.HasSecret()),
	)
}
