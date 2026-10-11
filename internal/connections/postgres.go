package connections

import (
	"context"
	"fmt"

	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/SandwichLabs/vibe/internal/session"
)

// postgresType attaches a PostgreSQL database through a temporary secret:
//
//	CREATE OR REPLACE TEMPORARY SECRET "vibe_x" (TYPE postgres, HOST '...', PORT 5432,
//	    DATABASE '...', USER '...', PASSWORD '...')
//	ATTACH '' AS "x" (TYPE postgres, SECRET "vibe_x" [, READ_ONLY])
//
// An inline DSN would put the password into duckdb_databases().path and is
// rejected by the hardened hub anyway (spike #107).
//
// READ_ONLY is a client-side guard in DuckDB: use a database role that can
// only read. postgres_execute / postgres_query / postgres_scan are denied to
// the agent by the SQL policy (#116).
type postgresType struct{}

func (postgresType) Name() string                      { return "postgres" }
func (postgresType) Remote() bool                      { return true }
func (postgresType) Extensions() []string              { return []string{"postgres"} }
func (postgresType) RefreshSQL() string                { return "CALL pg_clear_cache()" }
func (postgresType) NeedsCredential(session.Spec) bool { return true }

var sslModes = []string{"disable", "allow", "prefer", "require", "verify-ca", "verify-full"}

func (postgresType) Validate(s session.Spec) error {
	if err := noStrayFields(s, "host", "port", "database"); err != nil {
		return err
	}
	if err := requireField(s, "a host", param(s, "host")); err != nil {
		return err
	}
	if !hostRE.MatchString(param(s, "host")) {
		return fmt.Errorf("postgres connection %q: host %q is not a host name or address", s.Name, param(s, "host"))
	}
	if _, err := parsePort(s, 5432); err != nil {
		return err
	}
	if err := requireField(s, "a database", param(s, "database")); err != nil {
		return err
	}
	if err := unknownOptions(s, "sslmode"); err != nil {
		return err
	}
	return oneOf("sslmode", opt(s, "sslmode"), sslModes...)
}

func (t postgresType) SecretSteps(s session.Spec, c secrets.Credential) ([]session.SecretStep, error) {
	if err := t.Validate(s); err != nil {
		return nil, err
	}
	if c.User == "" {
		return nil, fmt.Errorf("postgres connection %q needs a user name", s.Name)
	}
	port, _ := parsePort(s, 5432)
	params := []session.SecretParam{
		{Key: "HOST", Value: param(s, "host")},
		{Key: "PORT", Value: port},
		{Key: "DATABASE", Value: param(s, "database")},
		{Key: "USER", Value: c.User},
		{Key: "PASSWORD", Value: c.Password, Secret: true},
	}
	return []session.SecretStep{{Name: secretName(s.Name), Type: "postgres", Params: params}}, nil
}

func (t postgresType) AttachSQL(s session.Spec) ([]string, error) {
	if err := t.Validate(s); err != nil {
		return nil, err
	}
	// sslmode is not a secret option, so it rides in the connection string. It
	// is validated against a closed list and is not a credential.
	// connect_timeout bounds a dead host to 10 s (libpq's default is to wait on
	// the OS), so a lazy connect cannot hang a query for minutes.
	conninfo := "connect_timeout=10"
	if m := opt(s, "sslmode"); m != "" {
		conninfo += " sslmode=" + m
	}
	stmt := "ATTACH " + session.QuoteLiteral(conninfo) + " AS " + session.QuoteIdent(s.Name) + " (TYPE postgres, SECRET " + session.QuoteIdent(secretName(s.Name))
	if s.ReadOnly {
		stmt += ", READ_ONLY"
	}
	return []string{stmt + ")"}, nil
}

func (postgresType) Probe(ctx context.Context, sess session.Session, s session.Spec) (int, string, error) {
	// A catalog listing: cheap (two server statements) and enough to prove the
	// login, the database and the permissions.
	var n int
	err := sess.DB().QueryRowContext(ctx, `SELECT count(*) FROM duckdb_tables()
		WHERE database_name = $1 AND schema_name NOT IN ('pg_catalog', 'information_schema')`, s.Name).Scan(&n)
	if err != nil {
		return 0, "", fmt.Errorf("connected, but listing tables failed: %w", err)
	}
	return n, fmt.Sprintf("%d tables", n), nil
}

func (postgresType) Info() TypeInfo {
	return TypeInfo{
		Name: "postgres", Label: "PostgreSQL",
		Fields: []Field{
			{Name: "host", Label: "Host", Kind: KindText, Required: true},
			{Name: "port", Label: "Port", Kind: KindNumber, Default: "5432"},
			{Name: "database", Label: "Database", Kind: KindText, Required: true},
			{Name: "sslmode", Label: "SSL mode", Kind: KindEnum, Choices: sslModes, Option: true, Help: "require or stronger for anything off your own machine"},
		},
		Credentials: []Field{
			{Name: "user", Label: "User name", Kind: KindText, Required: true, PerPerson: true},
			{Name: "password", Label: "Password", Kind: KindText, Secret: true, PerPerson: true},
		},
		Extensions:        []string{"postgres"},
		Capabilities:      Capabilities{Catalog: true, Writable: true, ReadOnlyAttach: false, Remote: true, Test: true},
		FingerprintFields: []string{"type", "host", "port", "database", "option:sslmode"},
		SignInMethods:     []string{"keychain", "env", "prompt"},
	}
}
