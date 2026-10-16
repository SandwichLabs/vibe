package session

import (
	"context"
	"fmt"
)

// ObjectLister is implemented by the hub: it counts the objects under a
// storage scope with a private session that still allows S3 globbing.
//
// The hub itself reads every S3-like URL literally (no ?s3_* parameters, no
// globs; storage_deny.go, #138), and DuckDB lists a bucket only through a glob.
// The one thing Vibe needs a listing for is Test's proof that a connection's
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
	ps, err := New(WithExtensionDirectory(h.cfg.extensionDir), WithRedactor(h.redactor), withListing())
	if err != nil {
		return 0, fmt.Errorf("failed to open the listing session: %w", err)
	}
	defer ps.Close() //nolint:errcheck
	inner, ok := ps.(*hub)
	if !ok {
		return 0, fmt.Errorf("failed to open the listing session")
	}
	if _, loaded, err := extensionState(ctx, inner.db, "httpfs"); err != nil || !loaded {
		return 0, fmt.Errorf("the httpfs extension is not available to list %s", pattern)
	}
	if err := inner.CreateTemporarySecret(ctx, step.Name, step.Type, step.Params); err != nil {
		return 0, err
	}
	var n int
	err = inner.db.QueryRowContext(ctx,
		`SELECT count(*) FROM (SELECT file FROM glob(`+QuoteLiteral(pattern)+`) LIMIT `+fmt.Sprint(limit)+`)`).Scan(&n)
	if err != nil {
		return 0, inner.wrapErr("could not list the scope", err)
	}
	return n, nil
}
