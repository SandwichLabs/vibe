package session

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
)

// Why this exists (#135). When httpfs loads, DuckDB copies the host's AWS_*
// environment (key id, secret, session token, region) into its s3_* settings,
// even in a lock_configuration session, and httpfs is loaded lazily, after the
// lock, so those settings can no longer be cleared with SET. A URL that no
// scoped secret covers would then be read, and signed, as the host's AWS
// identity.
//
// Secrets win over settings, and the secret with the longest matching scope
// wins. So every session that loads httpfs gets one lowest-priority "deny"
// secret per storage scheme: empty keys and an endpoint that never resolves
// (RFC 2606 .invalid). An uncovered URL fails to connect and is never signed.
// The endpoint alone is not the protection: httpfs lets a URL's own ?s3_endpoint=
// override it, so the secret also writes SESSION_TOKEN and REGION explicitly
// (DuckDB fills any omitted field from the host-filled settings). With empty
// keys and nothing host-derived left to fill in, nothing of the host's can be
// sent wherever a request ends up.
// The scope is the bare scheme ("s3", not "s3://"), one character group
// shorter than any real scope, so a connection whose scope is the whole
// scheme still wins instead of tying with the deny secret (a tie goes to the
// secret created first, which would be an accident of ordering).

// denyEndpoint never resolves.
const denyEndpoint = "deny.invalid"

// denySecrets are the catch-all secrets. Names contain a hyphen, which a
// connection name cannot, so no connection's own secret ("vibe_<name>")
// can share one.
var denySecrets = []struct{ name, kind, scope, region string }{
	{"vibe-deny-s3", "s3", "s3", "us-east-1"}, // also s3a:// and s3n://
	{"vibe-deny-r2", "r2", "r2", "auto"},
	{"vibe-deny-gcs", "gcs", "gcs", "auto"},
	{"vibe-deny-gs", "gcs", "gs", "auto"},
}

// denyStatements is the DDL, a pure function so it is testable without httpfs.
func denyStatements() []string {
	out := make([]string, 0, len(denySecrets))
	for _, d := range denySecrets {
		out = append(out, "CREATE TEMPORARY SECRET IF NOT EXISTS "+QuoteIdent(d.name)+
			" (TYPE "+d.kind+", KEY_ID '', SECRET '', SESSION_TOKEN '', REGION "+QuoteLiteral(d.region)+
			", ENDPOINT "+QuoteLiteral(denyEndpoint)+", SCOPE "+QuoteLiteral(d.scope)+")")
	}
	return out
}

// storageQuerier is what the deny step needs; *sql.DB and *sql.Conn both have it.
type storageQuerier interface {
	Execer
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// denyMu serialises the deny step: two first creations racing on different
// connections fail with a catalog write-write conflict even with IF NOT EXISTS.
var denyMu sync.Mutex

// DenyUncoveredStorage creates the catch-all deny secrets once httpfs is
// loaded. It is idempotent and a no-op until httpfs is loaded, so every place
// that loads an extension calls it afterwards. It runs on a connection of the
// session being protected, because secrets belong to the DuckDB instance.
func DenyUncoveredStorage(ctx context.Context, q storageQuerier) error {
	denyMu.Lock()
	defer denyMu.Unlock()
	_, loaded, err := extensionState(ctx, q, "httpfs")
	if err != nil {
		return fmt.Errorf("failed to inspect extensions: %w", err)
	}
	if !loaded {
		return nil
	}
	for _, stmt := range denyStatements() {
		if _, err := q.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("failed to protect storage reads from host credentials: %w", err)
		}
	}
	return nil
}

// prepareStorage runs while a session opens, before the lock: when httpfs is
// already installed it is loaded now and the deny secrets are created, so
// there is no window in which it is loaded without them.
func prepareStorage(ctx context.Context, conn *sql.Conn, log *slog.Logger) error {
	installed, loaded, err := extensionState(ctx, conn, "httpfs")
	if err != nil {
		return fmt.Errorf("failed to inspect extensions: %w", err)
	}
	if installed && !loaded {
		if _, err := conn.ExecContext(ctx, "LOAD httpfs"); err != nil {
			// A broken httpfs file must not stop the session opening: it is
			// reported as an error when a connection needs it. Not loaded
			// means the host settings are never filled either.
			log.Debug("httpfs could not be loaded while opening the session", "error", err)
			return nil
		}
	}
	return DenyUncoveredStorage(ctx, conn)
}
