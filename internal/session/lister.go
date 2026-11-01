package session

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/SandwichLabs/assay/internal/storageurl"
)

// ObjectLister is implemented by the hub: it counts the objects under a
// storage scope with a private session that still allows S3 globbing.
//
// The hub itself reads every S3-like URL literally (no ?s3_* parameters, no
// globs; storage_deny.go, #138), and DuckDB lists a bucket only through a glob.
// The one thing Assay needs a listing for is Test's proof that a connection's
// key, region and scope work, so that listing runs here, in a throwaway
// in-memory session that holds one connection's secret and is asked one
// question about a scope the caller has already vetted (no "?", no wildcard
// but the trailing one). Nothing a bucket holds is ever opened, so no URL that
// comes from data can reach it.
type ObjectLister interface {
	// ListObjects counts the objects whose URL matches pattern, up to limit.
	// pattern is the scope plus "**", built by the caller from a validated
	// scope. step is the connection's secret.
	ListObjects(ctx context.Context, step SecretStep, pattern string, limit int) (int, error)
}

var _ ObjectLister = (*hub)(nil)

// ListObjects implements ObjectLister.
func (h *hub) ListObjects(ctx context.Context, step SecretStep, pattern string, limit int) (int, error) {
	var n int
	err := h.withListing(ctx, step, pattern, func(db *sql.DB) error {
		return db.QueryRowContext(ctx,
			`SELECT count(*) FROM (SELECT file FROM glob(`+QuoteLiteral(pattern)+`) LIMIT `+fmt.Sprint(limit)+`)`).Scan(&n)
	})
	if err != nil {
		return 0, err
	}
	return n, nil
}

// ListMatching lists the objects an s3, gcs or r2 wildcard pattern matches, up
// to limit (one more than limit when there are more). It is what
// internal/storageglob uses to expand a wildcard (#138): the hub reads every
// S3-like URL literally and cannot glob, so the listing runs in the private
// session above, which holds the one secret of the connection whose scope
// covers the pattern.
//
// The pattern is validated before any session sees it (a plain URL, "*" and
// "**" only, no "?", so no URL parameter can reach the lister), and it must
// start with a ready storage connection's scope: the longest such scope wins,
// as it does when the hub reads. A pattern no connection covers is refused,
// never listed anonymously or with the host's credentials.
func (h *hub) ListMatching(ctx context.Context, pattern string, limit int) ([]string, error) {
	if err := storageurl.ValidatePattern(pattern); err != nil {
		return nil, fmt.Errorf("the pattern is not a plain URL: %w", err)
	}
	step, timeout, err := h.coveringSecret(ctx, pattern)
	if err != nil {
		return nil, err
	}
	// The listing is bound by the same statement timeout as a read.
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	var keys []string
	err = h.withListing(ctx, step, pattern, func(db *sql.DB) error {
		rows, err := db.QueryContext(ctx,
			`SELECT file FROM (SELECT file FROM glob(`+QuoteLiteral(pattern)+`) LIMIT `+fmt.Sprint(limit+1)+`) ORDER BY file`)
		if err != nil {
			return err
		}
		defer rows.Close() //nolint:errcheck
		for rows.Next() {
			var k string
			if err := rows.Scan(&k); err != nil {
				return err
			}
			keys = append(keys, k)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	h.logger.Debug("listed a storage pattern", "keys", len(keys))
	return keys, nil
}

// coveringSecret finds the ready storage connection whose scope covers the
// pattern (the longest scope wins) and builds its one secret from its own
// credential. It never looks at the host environment.
func (h *hub) coveringSecret(ctx context.Context, pattern string) (SecretStep, time.Duration, error) {
	var best *entry
	bestLen := -1
	var blocked *entry
	var blockedWhy string
	h.mu.RLock()
	for _, e := range h.conns {
		t, err := h.types.Get(e.spec.Type)
		if err != nil {
			continue
		}
		if ss, ok := t.(StorageScopeType); !ok || !ss.StorageScope() {
			continue
		}
		if !storageurl.PatternWithin(pattern, e.spec.Params["scope"]) {
			continue
		}
		if e.status.Kind != StatusOK && e.status.Kind != StatusNotConnected {
			blocked, blockedWhy = e, string(e.status.Kind)
			if e.status.Kind == StatusNeedsCredentials && e.status.Message != "" {
				// The status message is already redacted; for an AWS SSO sign-in it
				// says which profile to sign in with.
				blockedWhy += ": " + e.status.Message
			}
			continue
		}
		if n := len(e.spec.Params["scope"]); n > bestLen {
			best, bestLen = e, n
		}
	}
	h.mu.RUnlock()
	if best == nil {
		if blocked != nil {
			return SecretStep{}, 0, fmt.Errorf("the connection %q covers %s but is not ready (%s)", blocked.spec.Name, pattern, blockedWhy)
		}
		return SecretStep{}, 0, fmt.Errorf("no storage connection has a scope that covers %s; add one (its scope must start the pattern, with the same scheme: s3:// and s3a://, gcs:// and gs:// are different)", pattern)
	}
	step, err := h.secretFor(ctx, best.spec)
	timeout := best.spec.Timeout
	if timeout == 0 {
		timeout = h.cfg.defaultTO
	}
	return step, timeout, err
}

// secretFor builds the one secret a storage connection reads with.
func (h *hub) secretFor(ctx context.Context, spec Spec) (SecretStep, error) {
	t, err := h.types.Get(spec.Type)
	if err != nil {
		return SecretStep{}, err
	}
	ct, ok := t.(CredentialedType)
	if !ok || spec.Credential == nil {
		return SecretStep{}, fmt.Errorf("the connection %q has no credential to list with", spec.Name)
	}
	cred, err := spec.Credential(ctx)
	if err != nil {
		return SecretStep{}, fmt.Errorf("the connection %q has no usable credential: %s", spec.Name, h.redact(err.Error()))
	}
	steps, err := ct.SecretSteps(spec, cred)
	if err != nil {
		return SecretStep{}, fmt.Errorf("the connection %q cannot list: %s", spec.Name, h.redact(err.Error()))
	}
	if len(steps) != 1 {
		return SecretStep{}, fmt.Errorf("the connection %q has no single secret to list with", spec.Name)
	}
	return steps[0], nil
}

// withListing opens the private, hardened listing session, gives it the one
// secret, and runs fn on it. Nothing a bucket holds is opened there.
func (h *hub) withListing(ctx context.Context, step SecretStep, pattern string, fn func(db *sql.DB) error) error {
	// The listing session globs with URL parameters ON, so the pattern is
	// checked again here, whoever the caller is: a plain S3-like URL with no
	// "?", "[", escape or "..".
	if err := storageurl.ValidatePattern(pattern); err != nil {
		return fmt.Errorf("refusing to list %s: %w", pattern, err)
	}
	ps, err := New(WithExtensionDirectory(h.cfg.extensionDir), WithRedactor(h.redactor), withListing())
	if err != nil {
		return fmt.Errorf("failed to open the listing session: %w", err)
	}
	defer ps.Close() //nolint:errcheck
	inner, ok := ps.(*hub)
	if !ok {
		return fmt.Errorf("failed to open the listing session")
	}
	if _, loaded, err := extensionState(ctx, inner.db, "httpfs"); err != nil || !loaded {
		return fmt.Errorf("the httpfs extension is not available to list %s", pattern)
	}
	if err := inner.CreateTemporarySecret(ctx, step.Name, step.Type, step.Params); err != nil {
		return err
	}
	if err := fn(inner.db); err != nil {
		return inner.wrapErr("could not list "+strings.TrimSuffix(pattern, "**"), err)
	}
	return nil
}
