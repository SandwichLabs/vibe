package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/SandwichLabs/vibe/internal/secrets"
	"regexp"
	"strconv"
	"strings"
)

// redactedError carries a scrubbed message and the original cause for
// errors.Is / errors.As (context.DeadlineExceeded and friends keep working).
type redactedError struct {
	msg string
	err error
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.err }

func (h *hub) redact(s string) string { return h.redactor.Redact(s) }

// Redact implements Session.
func (h *hub) Redact(s string) string { return h.redact(s) }

// Redactor implements Session.
func (h *hub) Redactor() *secrets.Redactor { return h.redactor }

// wrapErr wraps a driver error with context and scrubs its text (DuckDB
// echoes connection parameters in some errors).
func (h *hub) wrapErr(ctx string, err error) error {
	return &redactedError{msg: ctx + ": " + h.redact(err.Error()), err: err}
}

// SecretParam is one option of a CREATE SECRET statement: Key is a bare
// option name (TYPE, KEY_ID, SERVER ...), Value a string, integer or bool.
//
// Secret marks a value that must be scrubbed everywhere. When false the key
// name decides (see secretBearingKeyRE: PASSWORD, SECRET, *TOKEN, ACCOUNT_KEY,
// CONNECTION_STRING, ...). Plain options (HOST, USER, DATABASE, REGION,
// KEY_ID, SCOPE, ENDPOINT) are never registered: doing so would mask
// "postgres" or "us-east-1" app-wide, and the user name stays readable.
type SecretParam struct {
	Key    string
	Value  any
	Secret bool
}

var secretBearingKeyRE = regexp.MustCompile(`(?i)(password|passwd|secret|token|header|account_key|connection_string|private_key|sas)`)

func (p SecretParam) bearsSecret() bool {
	return p.Secret || secretBearingKeyRE.MatchString(p.Key)
}

// secretError builds the only message the secret-DDL path ever returns: the
// DuckDB error class and the text before its first quote, "(values hidden)",
// and the option names involved. No quoted text from DuckDB is passed on (it
// can pair quotes wrongly and echo values of any length), and a prefix that
// contains any secret-bearing value, however short, is dropped.
func secretError(name string, err error, params []SecretParam) string {
	msg := err.Error()
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	class := msg
	if i := strings.Index(msg, ":"); i >= 0 {
		class = msg[:i]
	}
	prefix := msg
	if i := strings.IndexAny(msg, `'"`); i >= 0 {
		prefix = msg[:i]
	}
	for _, p := range params {
		if v, ok := p.Value.(string); ok && v != "" && p.bearsSecret() && strings.Contains(prefix, v) {
			prefix = class
			break
		}
	}
	names := make([]string, 0, len(params))
	for _, p := range params {
		names = append(names, p.Key)
	}
	return fmt.Sprintf("failed to create secret %q: %s (values hidden; options: %s)", name, strings.TrimSpace(prefix), strings.Join(names, ", "))
}

var secretKeyRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)

// secretSQL renders the statement and returns it with the string values it
// quoted. It never leaves the package: the text carries the secret.
func secretSQL(name, secretType string, params []SecretParam) (string, []string, error) {
	return secretSQLCore(name, secretType, params)
}

func secretSQLCore(name, secretType string, params []SecretParam) (string, []string, error) {
	if err := ValidateName(name); err != nil {
		return "", nil, fmt.Errorf("invalid secret name: %w", err)
	}
	if !secretKeyRE.MatchString(secretType) {
		return "", nil, fmt.Errorf("invalid secret type %q", secretType)
	}
	sql := "CREATE OR REPLACE TEMPORARY SECRET " + QuoteIdent(name) + " (TYPE " + secretType
	var values []string
	for _, p := range params {
		if !secretKeyRE.MatchString(p.Key) {
			return "", nil, fmt.Errorf("invalid secret option %q", p.Key)
		}
		sql += ", " + p.Key + " "
		switch v := p.Value.(type) {
		case string:
			sql += QuoteLiteral(v)
			if p.bearsSecret() {
				values = append(values, v)
			}
		case int:
			sql += strconv.Itoa(v)
		case bool:
			sql += strconv.FormatBool(v)
		default:
			return "", nil, fmt.Errorf("secret option %s: unsupported value type", p.Key)
		}
	}
	return sql + ")", values, nil
}

// CreateTemporarySecret implements Session. The statement runs on a
// dedicated connection under adminMu and is not logged; DuckDB's error text is
// scrubbed of the values and replaced when it could still quote the
// statement.
func (h *hub) CreateTemporarySecret(ctx context.Context, name, secretType string, params []SecretParam) error {
	h.adminMu.Lock()
	defer h.adminMu.Unlock()
	conn, err := h.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get admin connection: %w", err)
	}
	defer conn.Close() //nolint:errcheck
	return h.createSecretOn(ctx, conn, name, secretType, params)
}

// createSecretOn runs the secret DDL on conn; the caller holds adminMu.
func (h *hub) createSecretOn(ctx context.Context, conn *sql.Conn, name, secretType string, params []SecretParam) error {
	stmt, values, err := secretSQL(name, secretType, params)
	if err != nil {
		return err
	}
	h.redactor.Add(values...)
	if _, err := conn.ExecContext(ctx, stmt); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Every quoted literal goes, whatever its length or key: the statement
		// carries credentials and DuckDB may echo any part of it.
		return &redactedError{msg: h.redact(secretError(name, err, params)), err: errors.New("secret statement failed")}
	}
	h.logger.Debug("secret created", "name", name, "type", secretType)
	return nil
}

// DropSecret implements Session.
func (h *hub) DropSecret(ctx context.Context, name string) error {
	if err := ValidateName(name); err != nil {
		return fmt.Errorf("invalid secret name: %w", err)
	}
	h.adminMu.Lock()
	defer h.adminMu.Unlock()
	conn, err := h.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get admin connection: %w", err)
	}
	defer conn.Close() //nolint:errcheck
	if _, err := conn.ExecContext(ctx, "DROP TEMPORARY SECRET IF EXISTS "+QuoteIdent(name)); err != nil {
		return fmt.Errorf("failed to drop secret %q: %s", name, h.redact(err.Error()))
	}
	return nil
}
