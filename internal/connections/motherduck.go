package connections

import (
	"context"
	"errors"
	"fmt"

	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/SandwichLabs/vibe/internal/session"
)

// motherduckType attaches one MotherDuck database as one catalog:
//
//	ATTACH 'md:<database>' AS "x"
//
// The token is not part of any statement. DuckDB 1.4.1 has no
// CREATE SECRET (TYPE motherduck); the token is SET on the hub after LOAD and
// before lock_configuration, when the workspace opens (session
// WithMotherDuckToken), and md: is never attached without it, because that
// starts a browser SSO flow that blocks and writes to stdout (spike #107).
//
// READ_ONLY on md: is not supported (MotherDuck's docs), so a read-only
// MotherDuck definition is rejected (ErrMotherDuckReadOnly) instead of being
// silently writable; use a read-scaling token for read-only access.
//
// A named database attaches exactly that one database. The extension treats
// the whole string after "md:" as the database name, so a "?attach_mode=single"
// suffix is not accepted (#163: it failed with 'no database/share named
// "my_db?attach_mode=single"'); only a bare "md:" is workspace mode, which
// attaches every database of the account. Verified live on DuckDB 1.4.1.
type motherduckType struct{}

func (motherduckType) Name() string                      { return "motherduck" }
func (motherduckType) Remote() bool                      { return true }
func (motherduckType) Extensions() []string              { return []string{"motherduck"} }
func (motherduckType) NeedsCredential(session.Spec) bool { return true }
func (motherduckType) UsesHubToken() bool                { return true }

// ErrMotherDuckReadOnly is why a read-only MotherDuck connection is refused.
const ErrMotherDuckReadOnly = "MotherDuck connections can't be attached read-only; use a read-scaling token for read-only access"

func (motherduckType) Validate(s session.Spec) error {
	if s.ReadOnly {
		return errors.New(ErrMotherDuckReadOnly)
	}
	db := param(s, "database")
	if err := requireField(s, "a database", db); err != nil {
		return err
	}
	if !mdDBRE.MatchString(db) {
		return fmt.Errorf("motherduck connection %q: database %q may only use letters, digits, '_' and '-'", s.Name, db)
	}
	return unknownOptions(s)
}

// SecretSteps: none. The token is a hub-wide setting, not a secret.
func (t motherduckType) SecretSteps(s session.Spec, c secrets.Credential) ([]session.SecretStep, error) {
	if err := t.Validate(s); err != nil {
		return nil, err
	}
	if c.Token == "" {
		return nil, fmt.Errorf("motherduck connection %q needs a token", s.Name)
	}
	return nil, nil
}

func (t motherduckType) AttachSQL(s session.Spec) ([]string, error) {
	if err := t.Validate(s); err != nil {
		return nil, err
	}
	if param(s, "forced_read_only") != "" {
		return nil, errors.New(ErrMotherDuckReadOnly + " (this command opens the workspace read-only)")
	}
	// The database name is restricted to [A-Za-z0-9_-] by Validate (never
	// empty), so the literal cannot be bare "md:" (every database) and cannot
	// carry a '?' or '&' parameter of its own.
	return []string{"ATTACH " + session.QuoteLiteral("md:"+param(s, "database")) + " AS " + session.QuoteIdent(s.Name)}, nil
}

func (motherduckType) Probe(ctx context.Context, sess session.Session, s session.Spec) (int, string, error) {
	var n int
	err := sess.DB().QueryRowContext(ctx, `SELECT count(*) FROM duckdb_tables() WHERE database_name = $1`, s.Name).Scan(&n)
	if err != nil {
		return 0, "", fmt.Errorf("connected, but listing tables failed: %w", err)
	}
	return n, fmt.Sprintf("%d tables", n), nil
}

func (motherduckType) Info() TypeInfo {
	return TypeInfo{
		Name: "motherduck", Label: "MotherDuck",
		Fields: []Field{
			{Name: "database", Label: "Database", Kind: KindText, Required: true, Help: "One database is one catalog."},
		},
		Credentials: []Field{
			{Name: "token", Label: "Token", Kind: KindText, Required: true, Secret: true, PerPerson: true,
				Help: "A read-scaling token makes the connection read-only. env:MOTHERDUCK reads MOTHERDUCK_TOKEN."},
		},
		Extensions:        []string{"motherduck"},
		Capabilities:      Capabilities{Catalog: true, Writable: true, ReadOnlyAttach: false, Remote: true, Test: true, Subprocess: true},
		FingerprintFields: []string{"type", "database"},
		SignInMethods:     []string{"keychain", "env", "prompt"},
	}
}
