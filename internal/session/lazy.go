package session

// LazyStorage reports whether httpfs was not loaded when the session opened.
// Only then can the S3 URL-parameter setting change at all (the lock lets it
// through, see storage_deny.go), so it is the only state in which extra guards
// on the editor's SQL are worth their cost (#138).
func LazyStorage(s Session) bool {
	h, ok := s.(*hub)
	return ok && h.cfg.lazyStorage
}
