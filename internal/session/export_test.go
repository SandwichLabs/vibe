package session

import "database/sql"

// Wrap builds a Session over an existing DuckDB instance opened with OpenDB.
// It applies the SET-only hardening, locks the configuration and reads every
// option back; it fails closed if the instance was not opened hardened. The
// caller keeps ownership of db: Close does not close it.
func Wrap(db *sql.DB, opts ...Option) (Session, error) {
	c := newConfig(opts)
	applyDirDefaults(c)
	h, err := wrap(db, c, false)
	if err != nil {
		return nil, err
	}
	return h, nil
}

// CacheStats reports schema-cache hits and misses.
func CacheStats(s Session) (hits, misses int) {
	h, ok := s.(*hub)
	if !ok {
		return 0, 0
	}
	h.cache.mu.Lock()
	defer h.cache.mu.Unlock()
	return h.cache.hits, h.cache.misses
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
