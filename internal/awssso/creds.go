package awssso

import (
	"fmt"
	"log/slog"
	"time"
)

// Creds are short-lived role credentials. They live in process memory and in
// the DuckDB temporary secret only. Every way of printing one is redacted.
type Creds struct {
	KeyID   string
	Secret  string
	Token   string
	Expires time.Time
}

// SecretValues lists what must never be shown or logged (for the redactor).
func (c Creds) SecretValues() []string {
	var out []string
	for _, v := range []string{c.Secret, c.Token} {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

const redacted = "awssso.Creds{redacted}"

// String, GoString, Format, LogValue and MarshalJSON keep the values out of
// %v, %+v, %#v, %s, slog and JSON.
func (Creds) String() string   { return redacted }
func (Creds) GoString() string { return redacted }

// Format implements fmt.Formatter so no verb prints a field.
func (Creds) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(redacted)) }

// LogValue implements slog.LogValuer.
func (Creds) LogValue() slog.Value { return slog.StringValue(redacted) }

// MarshalJSON implements json.Marshaler.
func (Creds) MarshalJSON() ([]byte, error) { return []byte(`"` + redacted + `"`), nil }
