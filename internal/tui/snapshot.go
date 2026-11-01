package tui

import (
	"sync/atomic"

	"github.com/SandwichLabs/assay/internal/lsp"
)

// WithCatalogSnapshot keeps the catalog snapshot `assay lsp` completes from
// (.assay-local/catalog.json under the workspace root) current while the TUI owns the workspace: once on
// open and again after each REPL statement, which may have connected a
// connection or changed what exists.
func WithCatalogSnapshot(root string) Option {
	return func(a *App) { a.snapshotPath = root }
}

// refreshSnapshot rewrites the snapshot in the background; a refresh already
// running absorbs this one.
func (a *App) refreshSnapshot() {
	if a.snapshotPath == "" || a.ws == nil || a.ws.Session() == nil {
		return
	}
	if !a.snapshotBusy.CompareAndSwap(false, true) {
		return
	}
	sess, root, ctx := a.ws.Session(), a.snapshotPath, a.ctxOrBackground()
	go func() {
		defer a.snapshotBusy.Store(false)
		if _, err := lsp.Snapshot(ctx, sess, root); err != nil {
			a.logger.Debug("catalog snapshot incomplete", "error", err)
		}
	}()
}

// snapshotState is the App's snapshot bookkeeping.
type snapshotState struct {
	snapshotPath string
	snapshotBusy atomic.Bool
}
