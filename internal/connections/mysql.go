package connections

import (
	"context"
	"fmt"

	"github.com/SandwichLabs/vibe/internal/secrets"
	"github.com/SandwichLabs/vibe/internal/session"
)

// mysqlType attaches a MySQL (or MariaDB) database through a temporary secret,
// exactly like postgres:
//
//	CREATE OR REPLACE TEMPORARY SECRET "vibe_x" (TYPE mysql, HOST '...', PORT 3306,
//	    DATABASE '...', USER '...', PASSWORD '...' [, SSL_MODE '...'])
//	ATTACH '' AS "x" (TYPE mysql, SECRET "vibe_x" [, READ_ONLY])
//
// A connection string would put the password into duckdb_databases().path; the
// secret keeps it out of every statement the session can show.
//
// READ_ONLY is a client-side guard in DuckDB: use a MySQL account that can only
// SELECT. mysql_query / mysql_execute / mysql_clear_cache run raw statements on
// the server, outside every policy: they are denied to the agent in every tier
// (sql_names.go deniedFuncPrefix "mysql_").
type mysqlType struct{}

func (mysqlType) Name() string                      { return "mysql" }
func (mysqlType) Remote() bool                      { return true }
func (mysqlType) Extensions() []string              { return []string{"mysql"} }
func (mysqlType) RefreshSQL() string                { return "CALL mysql_clear_cache()" }
func (mysqlType) NeedsCredential(session.Spec) bool { return true }

// mysqlSSLModes are the values of the secret's SSL_MODE. Certificate and key
// file options (SSL_CA, SSL_CERT, ...) are deliberately not offered: they are
// file paths, and a path in a definition that is shared through git would read
// a file the definition's author chose.
var mysqlSSLModes = []string{"disabled", "preferred", "required", "verify_ca", "verify_identity"}

func (mysqlType) Validate(s session.Spec) error {
	if err := noStrayFields(s, "host", "port", "database"); err != nil {
		return err
	}
	if err := requireField(s, "a host", param(s, "host")); err != nil {
		return err
	}
	if !hostRE.MatchString(param(s, "host")) {
		return fmt.Errorf("mysql connection %q: host %q is not a host name or address", s.Name, param(s, "host"))
	}
	if _, err := parsePort(s, 3306); err != nil {
		return err
	}
	if err := requireField(s, "a database", param(s, "database")); err != nil {
		return err
	}
	if err := unknownOptions(s, "ssl_mode"); err != nil {
		return err
	}
	return oneOf("ssl_mode", opt(s, "ssl_mode"), mysqlSSLModes...)
}

func (t mysqlType) SecretSteps(s session.Spec, c secrets.Credential) ([]session.SecretStep, error) {
	if err := t.Validate(s); err != nil {
		return nil, err
	}
	if c.User == "" {
		return nil, fmt.Errorf("mysql connection %q needs a user name", s.Name)
	}
	port, _ := parsePort(s, 3306)
	params := []session.SecretParam{
		{Key: "HOST", Value: param(s, "host")},
		{Key: "PORT", Value: port},
		{Key: "DATABASE", Value: param(s, "database")},
		{Key: "USER", Value: c.User},
		{Key: "PASSWORD", Value: c.Password, Secret: true},
	}
	if m := opt(s, "ssl_mode"); m != "" {
		params = append(params, session.SecretParam{Key: "SSL_MODE", Value: m})
	}
	return []session.SecretStep{{Name: secretName(s.Name), Type: "mysql", Params: params}}, nil
}

// AttachSQL carries no host, user or password: they are all in the secret.
func (t mysqlType) AttachSQL(s session.Spec) ([]string, error) {
	if err := t.Validate(s); err != nil {
		return nil, err
	}
	stmt := "ATTACH '' AS " + session.QuoteIdent(s.Name) + " (TYPE mysql, SECRET " + session.QuoteIdent(secretName(s.Name))
	if s.ReadOnly {
		stmt += ", READ_ONLY"
	}
	return []string{stmt + ")"}, nil
}

func (mysqlType) Probe(ctx context.Context, sess session.Session, s session.Spec) (int, string, error) {
	var n int
	// The catalog lists every database the account can see (and MySQL's own
	// system ones); the connection's database is the one it is about.
	err := sess.DB().QueryRowContext(ctx, `SELECT count(*) FROM duckdb_tables() WHERE database_name = $1 AND schema_name = $2`,
		s.Name, param(s, "database")).Scan(&n)
	if err != nil {
		return 0, "", fmt.Errorf("connected, but listing tables failed: %w", err)
	}
	return n, fmt.Sprintf("%d tables", n), nil
}

func (mysqlType) Info() TypeInfo {
	return TypeInfo{
		Name: "mysql", Label: "MySQL",
		Fields: []Field{
			{Name: "host", Label: "Host", Kind: KindText, Required: true},
			{Name: "port", Label: "Port", Kind: KindNumber, Default: "3306"},
			{Name: "database", Label: "Database", Kind: KindText, Required: true},
			{Name: "ssl_mode", Label: "SSL mode", Kind: KindEnum, Choices: mysqlSSLModes, Option: true, Help: "required or stronger for anything off your own machine"},
		},
		Credentials: []Field{
			{Name: "user", Label: "User name", Kind: KindText, Required: true, PerPerson: true},
			{Name: "password", Label: "Password", Kind: KindText, Secret: true, PerPerson: true},
		},
		Extensions:        []string{"mysql"},
		Capabilities:      Capabilities{Catalog: true, Writable: true, ReadOnlyAttach: false, Remote: true, Test: true},
		FingerprintFields: []string{"type", "host", "port", "database", "option:ssl_mode"},
		SignInMethods:     []string{"keychain", "env", "prompt"},
	}
}
