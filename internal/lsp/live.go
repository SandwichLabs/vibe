package lsp

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/SandwichLabs/slaw/internal/session"
)

// MaxCompleteBytes is the longest SQL text Live.Complete looks at; a longer
// text returns no items rather than an error.
const MaxCompleteBytes = 512 << 10

const (
	liveDebounce     = 250 * time.Millisecond
	liveBuildTimeout = 20 * time.Second
	liveCloseWait    = 2 * time.Second
)

// CompleteResult is one completion answer. From and the items' replacement
// range are in UTF-16 code units (what an editor counts).
type CompleteResult struct {
	Items    []Item
	From     int
	Stale    []string // connections served from the saved snapshot, not the live schema
	Building bool     // a rebuild is pending or running
	// Incomplete means the list was cut at the item cap: the client must ask
	// again as the word grows rather than filter this list itself.
	Incomplete bool
}

// Live is the catalog Studio completes from: connected connections are rebuilt
// on demand, every other connection is served from the saved snapshot and
// marked stale. It never connects a connection: it reads only through the
// hub's pool and only for connections whose status is already ok.
type Live struct {
	sess     session.Session
	root     string // workspace root; "" = no snapshot
	write    bool   // write .slaw-local/catalog.json after a rebuild (this process owns the hub)
	caps     Caps
	logger   *slog.Logger
	debounce time.Duration
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{} // closed when the worker exits

	sem MetricLister // where the metrics come from; nil = the saved snapshot's

	mu      sync.RWMutex
	built   map[string]Connection // fresh entries by connection name
	saved   *Catalog              // the snapshot read at start; nil if none
	fns     []string
	metrics []MetricDoc // nil until read

	wmu sync.Mutex // serialises the snapshot write with Close

	dirtyMu sync.Mutex
	dirty   map[string]bool // names to rebuild; "" = every connected one
	running bool
	kick    chan struct{} // buffered(1); wakes the worker
}

// LiveOption configures NewLive.
type LiveOption func(*Live)

// WithLiveLogger sets the logger (debug-level rebuild errors).
func WithLiveLogger(l *slog.Logger) LiveOption { return func(v *Live) { v.logger = l } }

// WithLiveCaps overrides DefaultCaps().
func WithLiveCaps(c Caps) LiveOption { return func(v *Live) { v.caps = c.orDefault() } }

// WithLiveSnapshot reads the saved snapshot under root for the stale fallback
// and, when write is true, rewrites it after every rebuild.
func WithLiveSnapshot(root string, write bool) LiveOption {
	return func(v *Live) { v.root, v.write = root, write }
}

// WithLiveSemantics reads the metrics from src (a semantics.Store) on every
// rebuild; InvalidateSemantics queues one when a semantics/ file changes.
func WithLiveSemantics(src MetricLister) LiveOption { return func(v *Live) { v.sem = src } }

// semanticsKey is the dirty-set entry for "reload the metrics": not a valid
// connection name, so it can never collide with one.
const semanticsKey = "\x00semantics"

// InvalidateSemantics queues a reload of the metrics (a file under semantics/
// changed). It never blocks.
func (l *Live) InvalidateSemantics() { l.Invalidate(semanticsKey) }

// NewLive starts the rebuild worker and queues a first build of every
// connection that is connected now. Close it before closing sess.
func NewLive(sess session.Session, opts ...LiveOption) *Live {
	l := &Live{
		sess: sess, caps: DefaultCaps(), debounce: liveDebounce, logger: slog.New(slog.DiscardHandler),
		built: map[string]Connection{}, dirty: map[string]bool{},
		kick: make(chan struct{}, 1), done: make(chan struct{}),
	}
	l.ctx, l.cancel = context.WithCancel(context.Background())
	for _, o := range opts {
		o(l)
	}
	if l.root != "" {
		if c, err := Load(SnapshotPath(l.root)); err == nil {
			l.saved = c
		}
	}
	go l.worker()
	l.Invalidate("")
	return l
}

// Invalidate queues a rebuild of one connection ("" = every connected one). It
// never blocks and never takes a lock a caller might hold.
func (l *Live) Invalidate(name string) {
	if l == nil || l.ctx.Err() != nil {
		return
	}
	l.dirtyMu.Lock()
	l.dirty[name] = true
	l.dirtyMu.Unlock()
	select {
	case l.kick <- struct{}{}:
	default:
	}
}

// Building reports whether a rebuild is pending or running.
func (l *Live) Building() bool {
	l.dirtyMu.Lock()
	defer l.dirtyMu.Unlock()
	return l.running || len(l.dirty) > 0
}

// Close stops the worker, cancelling a rebuild in flight, and waits briefly
// for it. Call it before the session closes.
func (l *Live) Close() {
	if l == nil {
		return
	}
	l.cancel()
	// Once Close has taken and released wmu, no write can start (each checks
	// ctx under wmu), and one in flight has finished.
	l.wmu.Lock()
	l.wmu.Unlock() //nolint:staticcheck // empty critical section is the barrier
	select {
	case <-l.done:
	case <-time.After(liveCloseWait):
	}
}

func (l *Live) worker() {
	defer close(l.done)
	for {
		select {
		case <-l.ctx.Done():
			return
		case <-l.kick:
		}
		select {
		case <-l.ctx.Done():
			return
		case <-time.After(l.debounce):
		}
		l.dirtyMu.Lock()
		names := l.dirty
		l.dirty = map[string]bool{}
		l.running = len(names) > 0
		l.dirtyMu.Unlock()
		if len(names) > 0 {
			l.rebuild(names)
		}
		l.dirtyMu.Lock()
		l.running = false
		l.dirtyMu.Unlock()
	}
}

// rebuild reads the schema of every named (or, for "", every) connection that
// is connected now, and drops the ones that no longer are.
func (l *Live) rebuild(names map[string]bool) {
	ctx, cancel := context.WithTimeout(l.ctx, liveBuildTimeout)
	defer cancel()
	all := names[""]
	infos := catalogless(l.sess)
	def := l.sess.DefaultCatalog()
	built := map[string]Connection{}
	gone := map[string]bool{}
	for _, st := range l.sess.Connections() {
		if !all && !names[st.Name] {
			continue
		}
		if !completable(st, infos[st.Name], def) {
			gone[st.Name] = true
			continue
		}
		conn, err := buildConnection(ctx, l.sess, st, l.caps)
		if err != nil {
			l.logger.Debug("live catalog: build failed", "connection", st.Name, "error", err)
			continue
		}
		built[st.Name] = conn
	}
	fns, fnErr := l.functionsFor(ctx, all)
	if fnErr != nil {
		l.logger.Debug("live catalog: functions failed", "error", fnErr)
	}
	var docs []MetricDoc
	if l.sem != nil && (all || names[semanticsKey]) {
		var merr error
		if docs, merr = loadMetricDocs(ctx, l.sem); merr != nil {
			l.logger.Debug("live catalog: metrics failed", "error", merr)
			docs = nil
		}
	}
	l.mu.Lock()
	for n := range gone {
		l.forgetLocked(n)
	}
	if all { // a full rebuild also forgets connections that were unregistered
		registered := map[string]bool{}
		for _, st := range l.sess.Connections() {
			registered[st.Name] = true
		}
		for n := range l.built {
			if !registered[n] {
				l.forgetLocked(n)
			}
		}
	}
	for n, c := range built {
		l.built[n] = c
	}
	if fns != nil {
		l.fns = fns
	}
	if docs != nil {
		l.metrics = docs
	}
	snap := l.snapshotLocked(def)
	l.mu.Unlock()
	if (len(built) > 0 || docs != nil) && l.write && l.root != "" {
		l.writeSnapshot(snap)
	}
}

// forgetLocked drops a built entry (the connection went away or is no longer
// connected), keeping what it knew as the saved, stale entry. The caller holds
// l.mu.
func (l *Live) forgetLocked(name string) {
	if conn, ok := l.built[name]; ok {
		l.saved = l.saved.Merge(&Catalog{Version: catalogVersion, Connections: []Connection{conn}})
		delete(l.built, name)
	}
}

// writeSnapshot saves snap unless Close has begun: a Live that was closed
// (the workspace's hub released) must never write.
func (l *Live) writeSnapshot(snap *Catalog) {
	l.wmu.Lock()
	defer l.wmu.Unlock()
	if l.ctx.Err() != nil {
		return
	}
	if err := writeMerged(l.root, snap); err != nil {
		l.logger.Debug("live catalog: snapshot not written", "error", err)
	}
}

func (l *Live) functionsFor(ctx context.Context, all bool) ([]string, error) {
	l.mu.RLock()
	have := l.fns != nil
	l.mu.RUnlock()
	if have && !all {
		return nil, nil
	}
	return functionNames(ctx, l.sess)
}

// snapshotLocked is the fresh part of the catalog (built entries only) in the
// shape Merge writes. The caller holds l.mu.
func (l *Live) snapshotLocked(def string) *Catalog {
	c := &Catalog{Version: catalogVersion, Generated: time.Now().UTC(), DefaultConnection: def, Functions: l.fns, Metrics: l.metrics}
	for _, conn := range l.built {
		c.Connections = append(c.Connections, conn)
	}
	sort.Slice(c.Connections, func(i, j int) bool { return c.Connections[i].Name < c.Connections[j].Name })
	return c
}

// Catalog returns the merged view completion reads: the live entry of every
// connected connection, the saved entry (Stale) of every other registered one,
// and at least the name of a connection nothing is known about. Treat it as
// read-only; it shares slices with Live.
func (l *Live) Catalog() *Catalog {
	def := l.sess.DefaultCatalog()
	infos := catalogless(l.sess)
	sts := l.sess.Connections()
	l.mu.RLock()
	defer l.mu.RUnlock()
	c := &Catalog{Version: catalogVersion, Generated: time.Now().UTC(), DefaultConnection: def, Functions: l.fns}
	if c.Functions == nil && l.saved != nil {
		c.Functions = l.saved.Functions
	}
	c.Metrics = l.metrics
	if c.Metrics == nil && l.saved != nil {
		c.Metrics = l.saved.Metrics
	}
	for _, st := range sts {
		if infos[st.Name] || (builtinCatalog(st.Name) && st.Name != def) {
			continue
		}
		if conn, ok := l.built[st.Name]; ok {
			conn.Stale = st.Kind != session.StatusOK
			c.Connections = append(c.Connections, conn)
			continue
		}
		conn := Connection{Name: st.Name, Type: st.Type}
		if old := l.saved.Conn(st.Name); old != nil {
			conn = *old
		}
		conn.Stale = true
		c.Connections = append(c.Connections, conn)
	}
	return c
}

// Complete completes text at the UTF-16 offset. connection, when it names a
// known connection, is where unqualified names resolve (the editor's Run-on or
// the cell's connection); "" keeps the workspace default.
func (l *Live) Complete(text string, offset int, connection string) CompleteResult {
	res := CompleteResult{Building: l.Building()}
	if len(text) > MaxCompleteBytes {
		return res
	}
	cat := l.Catalog()
	if connection != "" && cat.Conn(connection) != nil {
		cat.DefaultConnection = cat.Conn(connection).Name
	}
	b := utf16ToByte(text, offset)
	items, from, incomplete := completeAt(text, b, cat)
	res.Items, res.Incomplete = items, incomplete
	res.From = byteToUTF16(text, from)
	for _, c := range cat.Connections {
		if c.Stale {
			res.Stale = append(res.Stale, c.Name)
		}
	}
	return res
}
