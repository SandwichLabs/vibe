package session

import (
	"fmt"
	"github.com/sandwich-labs/vibe-app/internal/secrets"
	"regexp"
	"strings"
	"time"
)

// StatusKind is a connection's state (plan §4.2). Lazy attach means a freshly
// registered connection is StatusNotConnected until first use.
type StatusKind string

const (
	StatusOK               StatusKind = "ok"
	StatusNotConnected     StatusKind = "not-connected"
	StatusLockedBy         StatusKind = "locked-by"
	StatusError            StatusKind = "error"
	StatusNeedsCredentials StatusKind = "needs-credentials"
	StatusExtensionMissing StatusKind = "extension-missing"
)

// precedence is the plan §4.2 (design #106) ordering, highest first:
// extension-missing > needs-credentials > error > locked-by > not connected > ok.
var precedence = []StatusKind{
	StatusExtensionMissing, StatusNeedsCredentials, StatusError,
	StatusLockedBy, StatusNotConnected, StatusOK,
}

// ResolveStatus returns the status that wins when several apply. An empty
// input is StatusNotConnected; an unknown kind ranks below every known one.
func ResolveStatus(kinds ...StatusKind) StatusKind {
	best, bestRank := StatusNotConnected, len(precedence)+1
	for _, k := range kinds {
		r := len(precedence)
		for i, p := range precedence {
			if p == k {
				r = i
			}
		}
		if r < bestRank {
			best, bestRank = k, r
		}
	}
	return best
}

// Status describes one connection. Message is already redacted; it never
// carries a credential, DSN or path.
type Status struct {
	Name        string     `json:"name"`
	Type        string     `json:"type"`
	Kind        StatusKind `json:"status"`
	Message     string     `json:"message,omitempty"`
	LockedByPID int        `json:"locked_by_pid,omitempty"`
	ReadOnly    bool       `json:"read_only"`
	Default     bool       `json:"default,omitempty"`
}

// Spec declares a connection. For the duckdb type, Path is the file to attach.
// Timeout / RowCap are this connection's statement timeout and row cap
// defaults (0 = none).
type Spec struct {
	Name     string        `json:"name"`
	Type     string        `json:"type"`
	Path     string        `json:"path,omitempty"`
	ReadOnly bool          `json:"read_only,omitempty"`
	Timeout  time.Duration `json:"-"`
	RowCap   int           `json:"-"`
}

var nameRE = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

// ValidateName enforces the catalog-alias rule from plan §4.2.
func ValidateName(name string) error {
	if !nameRE.MatchString(name) {
		return fmt.Errorf("invalid connection name %q: must match %s", name, nameRE.String())
	}
	return nil
}

// CatalogInfo is one attached catalog.
type CatalogInfo struct {
	Name     string `json:"name"`
	Type     string `json:"type"` // DuckDB's engine type: duckdb, postgres, sqlite, ...
	ReadOnly bool   `json:"read_only"`
	Default  bool   `json:"default,omitempty"`
}

// TableInfo is one table or view.
type TableInfo struct {
	Catalog string `json:"catalog"`
	Schema  string `json:"schema"`
	Name    string `json:"name"`
	Type    string `json:"type"` // BASE TABLE | VIEW | ...
}

// Ref returns the table's qualified reference.
func (t TableInfo) Ref() TableRef {
	return TableRef{Catalog: t.Catalog, Schema: t.Schema, Name: t.Name}
}

// ColumnInfo is one column.
type ColumnInfo struct {
	Name     string `json:"name"`
	DataType string `json:"data_type"`
	NotNull  bool   `json:"not_null"`
	Position int    `json:"position"`
}

// TableDescription is a resolved table and its columns.
type TableDescription struct {
	Ref     TableRef     `json:"ref"`
	Columns []ColumnInfo `json:"columns"`
}

var pidRE = regexp.MustCompile(`\(PID (\d+)\)`)

// Redact strips credential-shaped values from a DuckDB error or message
// (spike #107: Postgres connect errors echo password='...' verbatim) before it
// reaches a log, the UI or the agent. It knows patterns only; a Session's own
// Redact also scrubs the secret values it has seen.
func Redact(s string) string { return secrets.RedactPatterns(s) }

// classifyAttachError maps a DuckDB ATTACH failure to a status.
func classifyAttachError(err error, redact func(string) string) (StatusKind, string, int) {
	msg := redact(err.Error())
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "could not set lock on file") {
		pid := 0
		if m := pidRE.FindStringSubmatch(msg); m != nil {
			fmt.Sscanf(m[1], "%d", &pid) //nolint:errcheck
		}
		return StatusLockedBy, msg, pid
	}
	if strings.Contains(lower, "extension") && (strings.Contains(lower, "not found") || strings.Contains(lower, "not installed") || strings.Contains(lower, "to be loaded")) {
		return StatusExtensionMissing, msg, 0
	}
	return StatusError, msg, 0
}
