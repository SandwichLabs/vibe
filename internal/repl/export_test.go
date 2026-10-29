package repl

// Test-only accessors (kept out of the production API so deadcode stays quiet).

// Running reports whether a statement is being handled.
func (s *Session) Running() bool { return s.running.Load() }

// Current is the catalog the pinned connection works in.
func (s *Session) Current() string { return s.current }

// Buffered is the statement text typed so far and not yet complete.
func (s *Session) Buffered() string { return s.buf }

// ConnCatalog is the catalog the pinned connection reports ("" = default).
func (s *Session) ConnCatalog() string { return s.conn.Catalog() }
