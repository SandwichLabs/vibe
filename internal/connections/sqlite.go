package connections

import (
	"context"
	"fmt"
	"os"

	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/SandwichLabs/vibe/internal/session"
)

// sqliteType attaches a SQLite file: ATTACH 'path' AS x (TYPE sqlite[, READ_ONLY]).
// The path is resolved and contained by the workspace (the same rules as a
// duckdb file) before it reaches here.
type sqliteType struct{}

func (sqliteType) Name() string                      { return "sqlite" }
func (sqliteType) Remote() bool                      { return false }
func (sqliteType) Extensions() []string              { return []string{"sqlite"} }
func (sqliteType) NeedsCredential(session.Spec) bool { return false }

func (sqliteType) Validate(s session.Spec) error {
	if err := noStrayFields(s, "path"); err != nil {
		return err
	}
	if err := requireField(s, "a path", s.Path); err != nil {
		return err
	}
	return unknownOptions(s)
}

func (sqliteType) SecretSteps(session.Spec, secrets.Credential) ([]session.SecretStep, error) {
	return nil, nil
}

func (t sqliteType) AttachSQL(s session.Spec) ([]string, error) {
	if err := t.Validate(s); err != nil {
		return nil, err
	}
	if _, err := os.Stat(s.Path); err != nil {
		// SQLite would create an empty database for a mistyped path.
		return nil, fmt.Errorf("SQLite file for %q is not readable: %w", s.Name, err)
	}
	stmt := "ATTACH " + session.QuoteLiteral(s.Path) + " AS " + session.QuoteIdent(s.Name) + " (TYPE sqlite"
	if s.ReadOnly {
		stmt += ", READ_ONLY"
	}
	return []string{stmt + ")"}, nil
}

func (sqliteType) Probe(ctx context.Context, sess session.Session, s session.Spec) (int, string, error) {
	var n int
	err := sess.DB().QueryRowContext(ctx, `SELECT count(*) FROM duckdb_tables() WHERE database_name = $1`, s.Name).Scan(&n)
	if err != nil {
		return 0, "", fmt.Errorf("attached, but listing tables failed: %w", err)
	}
	return n, fmt.Sprintf("%d tables", n), nil
}

func (sqliteType) Info() TypeInfo {
	return TypeInfo{
		Name: "sqlite", Label: "SQLite file",
		Fields: []Field{
			{Name: "path", Label: "File", Kind: KindPath, Required: true, Help: "Inside the workspace for a shared connection."},
		},
		Extensions:        []string{"sqlite"},
		Capabilities:      Capabilities{Catalog: true, Writable: true, ReadOnlyAttach: true, Test: true},
		FingerprintFields: []string{"type", "path"},
	}
}
