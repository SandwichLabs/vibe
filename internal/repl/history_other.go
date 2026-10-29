//go:build !unix

package repl

// oNoFollow does nothing where there is no O_NOFOLLOW; safeDir and the
// regular-file check still refuse links.
const oNoFollow = 0
