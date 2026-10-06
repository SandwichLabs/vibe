// Package bundle reads and writes `.vibe` bundles, format version 2 (#118,
// plan section 4.6): a zip of a workspace's notebooks, queries, skills and
// (optionally) connection definitions, with a manifest that carries a SHA-256
// for every entry.
//
// A bundle holds no data and no credentials. There is no data/ directory and
// no .duckdb file; connection definitions are stripped of sign-in methods,
// absolute and out-of-workspace paths, ${ENV} references and credential-like
// options; per-person settings (sign-in choices, fingerprints, nonces, agent
// loosening) are never read in the first place.
//
// A bundle may be wrapped whole in an age envelope (filippo.io/age, scrypt
// passphrase recipient). That is the only cryptography here, and none of it is
// bespoke. The age header is the unencrypted flag: Peek tells a caller to
// prompt for a passphrase before anything else is attempted.
//
// Import treats the file as hostile. Every entry name is checked against a
// closed set of patterns (zip-slip, absolute paths, symlinks, unknown entries
// are refused), every entry is size-capped and the total unpacked size is
// capped (zip bombs), every SHA-256 is verified, and every document is parsed
// by its own store's rules before anything is written. Writes go through the
// stores, so overwrite is a compare-and-write and imported skills can never
// claim to be built-in.
//
// Single-notebook .vnb import and export share CheckNotebook with this path.
package bundle
