package main

// The catalog tree, table capabilities, and what a run changed.

import (
	"context"
	"errors"

	"github.com/SandwichLabs/vibe/internal/agents"
	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/session"
)

// CatalogRequest, CatalogNode and CatalogListing are one lazy level of a
// connection's tree: its schemas, a schema's tables and views, or a table's
// columns. The tree is built by engine.ListCatalog.
type (
	CatalogRequest = engine.CatalogRequest
	CatalogNode    = engine.CatalogNode
	CatalogListing = engine.CatalogListing
)

// ListCatalog returns one lazy level of a connection's tree. It never dials: a
// connection that is not connected is an error the rail answers by connecting
// first.
func (a *App) ListCatalog(req CatalogRequest) (*CatalogListing, error) {
	_, sess, _, err := a.snapshot()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(a.ctxOrBackground(), connTimeout)
	defer cancel()
	out, err := engine.ListCatalog(ctx, sess, req)
	if err != nil {
		return nil, a.catalogErr(err)
	}
	return out, nil
}

// catalogErr redacts a catalog error for the UI; the "no such connection"
// answer names only what the caller sent and passes through.
func (a *App) catalogErr(err error) error {
	var unknown *engine.UnknownConnectionError
	if errors.As(err, &unknown) {
		return err
	}
	return errors.New(a.redact(err.Error()))
}

// TableCapabilities says whether a table can be edited and, when not, why.
type TableCapabilities = engine.TableCapabilities

// GetTableCapabilities explains why a table is read-only, for the datasheet's
// banner (SPEC 07 E). It never dials.
func (a *App) GetTableCapabilities(table string) (*TableCapabilities, error) {
	_, sess, ro, err := a.snapshot()
	if err != nil {
		return nil, err
	}
	return engine.DescribeTableCapabilities(a.ctxOrBackground(), sess, table, ro)
}

// statusSnapshot records every connection's state, to tell after a run which
// ones a statement dialled (lazy attach) or changed.
func statusSnapshot(sess session.Session) map[string]session.StatusKind {
	out := map[string]session.StatusKind{}
	for _, st := range sess.Connections() {
		out[st.Name] = st.Kind
	}
	return out
}

// afterRun tells the rail what a run changed: a connection that attached (or
// failed) gets its status refreshed, and anything that is not a plain read, or
// that attached something, emits catalog:changed so open levels reload.
func (a *App) afterRun(sess session.Session, before map[string]session.StatusKind, sqlText string) {
	changed := a.settleRun(sess, before)
	if changed || !agents.IsReadOnlyStatement(a.ctxOrBackground(), sess.DB(), sqlText, nil) {
		a.emitEvent("catalog:changed", map[string]any{"conn": ""})
	}
}

// afterReadOnlyRun is afterRun for a run already vetted as a plain read (an
// artifact query): the S3 URL setting and connection status are settled as
// usual, but the catalog is only reported changed when a connection attached.
func (a *App) afterReadOnlyRun(sess session.Session, before map[string]session.StatusKind) {
	if a.settleRun(sess, before) {
		a.emitEvent("catalog:changed", map[string]any{"conn": ""})
	}
}

// settleRun re-checks the S3 URL setting and publishes any connection status
// that moved; it reports whether one did.
func (a *App) settleRun(sess session.Session, before map[string]session.StatusKind) bool {
	// Defence in depth (#138): whatever the run did, the S3 URL setting is on
	// again before the next one. A no-op unless httpfs is loaded. Residual: a
	// session-level override on the editor's own pinned connection is reset only
	// if the pool hands that connection back here; reaching one needs both the
	// text guard and the parser guard (usersql.CheckParsed) to have missed it.
	if err := session.DenyUncoveredStorage(a.ctxOrBackground(), sess.DB()); err != nil && a.logger != nil {
		a.logger.Warn("could not re-check the S3 URL setting after a run", "error", err)
	}
	changed := false
	for _, st := range sess.Connections() {
		if before[st.Name] != st.Kind {
			changed = true
			a.emitStatus(sess, st.Name)
		}
	}
	return changed
}
