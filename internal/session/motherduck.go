package session

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
)

// MotherDuck is special (spike #107 finding 3): there is no
// CREATE SECRET (TYPE motherduck) on DuckDB 1.4.1, the token is not a DSN
// option, and it only works as the motherduck_token environment variable or as
// SET motherduck_token after LOAD and before lock_configuration. With no token
// anywhere, ATTACH 'md:...' starts an interactive browser SSO flow, blocks
// about 60 s and writes to stdout (corrupting an MCP stdio stream). So:
//
//   - the hub receives the token when it opens (WithMotherDuckToken);
//   - the ambient env variables are removed first;
//   - nothing ever attaches md: without that token having been SET.

// mdEnvVars are the ambient variables the extension would read at LOAD.
var mdEnvVars = []string{"MOTHERDUCK_TOKEN", "motherduck_token"}

// ScrubMotherDuckEnv removes MotherDuck's ambient token variables from this
// process's environment. It reports which were set (names only).
func ScrubMotherDuckEnv() []string {
	var was []string
	for _, k := range mdEnvVars {
		if _, ok := os.LookupEnv(k); ok {
			was = append(was, k)
			_ = os.Unsetenv(k)
		}
	}
	return was
}

// mdOpen records what the hub did for MotherDuck at open.
type mdOpen struct {
	tokenID string // identity of the SET token ("" = none was SET)
	loaded  bool   // the extension is loaded in this hub
}

// TokenID is a short one-way identity of a MotherDuck token, used to tell
// whether two credentials are the same account without keeping the token.
func TokenID(token string) string {
	sum := sha256.Sum256([]byte("vibe-md-token\x00" + token))
	return hex.EncodeToString(sum[:])[:16]
}

// extensionState reads installed/loaded for an extension name or alias.
func extensionState(ctx context.Context, q interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}, name string) (installed, loaded bool, err error) {
	err = q.QueryRowContext(ctx,
		`SELECT coalesce(bool_or(installed), false), coalesce(bool_or(loaded), false)
		   FROM duckdb_extensions()
		  WHERE extension_name = $1 OR list_contains(aliases, $1)`, name).Scan(&installed, &loaded)
	return installed, loaded, err
}

// prepareMotherDuck runs inside applyHardening, before lock_configuration.
func prepareMotherDuck(ctx context.Context, conn *sql.Conn, c *config) error {
	if !c.mdSet {
		return nil
	}
	ScrubMotherDuckEnv()
	c.redactor.Add(c.mdToken)
	installed, _, err := extensionState(ctx, conn, "motherduck")
	if err != nil {
		return fmt.Errorf("failed to inspect extensions: %w", err)
	}
	c.md = &mdOpen{}
	if !installed {
		return nil // reported as extension-missing on first connect
	}
	if _, err := conn.ExecContext(ctx, "LOAD motherduck"); err != nil {
		return fmt.Errorf("failed to load the motherduck extension: %s", c.redactor.Redact(err.Error()))
	}
	if _, err := conn.ExecContext(ctx, "SET motherduck_token = "+QuoteLiteral(c.mdToken)); err != nil {
		return fmt.Errorf("failed to set the MotherDuck token: %s", c.redactor.Redact(err.Error()))
	}
	c.md.tokenID, c.md.loaded = TokenID(c.mdToken), true
	return nil
}

// RelaunchRequired implements Session.
func (h *hub) RelaunchRequired() (bool, string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.relaunch != "", h.relaunch
}

func (h *hub) setRelaunch(msg string) {
	h.mu.Lock()
	h.relaunch = msg
	h.mu.Unlock()
}

// checkHubToken compares a resolved MotherDuck token with the one the hub
// opened with. A non-empty return is the reason it cannot be used here; the
// workspace must be reopened (D10: one account per workspace).
func (h *hub) checkHubToken(token string) string {
	switch {
	case h.md == nil || h.md.tokenID == "":
		return "MotherDuck was not ready when this workspace opened (no token yet, or its extension was not installed), and a token can only be set while a workspace opens. Reopen the workspace to use it."
	case TokenID(token) != h.md.tokenID:
		return "This workspace opened with a different MotherDuck account, and one workspace uses one account. Reopen the workspace to switch."
	}
	return ""
}
