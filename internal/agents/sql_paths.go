package agents

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/SandwichLabs/vibe/internal/filestore"
	"github.com/SandwichLabs/vibe/internal/storageurl"
)

// pathPolicy decides which files and URLs agent SQL may read or write (#116).
//
//   - A file is readable only when it is written out as a plain string literal,
//     is an absolute path, and (after resolving symlinks) lies inside the
//     workspace's data/ folder.
//   - A URL is readable only when it lies inside the scope of a ready storage
//     connection (s3, gcs, r2, azure): a literal prefix match, with no "..", query or
//     fragment, so it cannot wander to another bucket or object.
//   - COPY ... TO may only write inside data/.
//
// Relative paths are refused: DuckDB resolves them against the process's
// working directory, which is not the workspace, so a path vetted as
// "data/x.csv" could read a different file when run.
type pathPolicy struct {
	dataDir string   // symlink-resolved absolute path of the workspace data/ folder; "" = no file access
	scopes  []string // normalised URL prefixes of ready storage connections
	// protected are files and folders COPY ... TO never writes into: the files
	// of attached connections (a DuckLake's catalog and its data folder, a duckdb
	// or sqlite file), symlink-resolved.
	protected []string
}

var urlSchemeRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.\-]*:`)

// replacementScanExts are the file extensions DuckDB (and the extensions this
// app loads) turn into a table scan when they appear where a table name goes.
var replacementScanExts = []string{
	".csv", ".tsv", ".txt", ".parquet", ".pq", ".json", ".jsonl", ".ndjson", ".gz", ".zst",
	".db", ".sqlite", ".sqlite3", ".duckdb", ".xlsx", ".xls", ".arrow", ".feather", ".avro",
	".orc", ".shp", ".geojson", ".gpkg", ".lance", ".delta",
}

// looksLikeFile reports whether a name that did not resolve to a table is
// shaped like a file or URL, so DuckDB would try a replacement scan on it.
func looksLikeFile(name string) bool {
	if strings.ContainsAny(name, `/\*?`) || strings.Contains(name, "://") {
		return true
	}
	lower := strings.ToLower(name)
	for _, e := range replacementScanExts {
		if strings.HasSuffix(lower, e) {
			return true
		}
	}
	return false
}

func hasBadPathChars(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// hasDotDot reports whether any path segment (split on / and \) is "..".
func hasDotDot(s string) bool {
	for _, seg := range strings.FieldsFunc(s, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." {
			return true
		}
	}
	return false
}

// realPath resolves symlinks as far as the path exists, re-attaching the part
// that does not.
func realPath(p string) (string, error) {
	p = filepath.Clean(p)
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r, nil
	}
	dir, base := filepath.Split(p)
	dir = filepath.Clean(dir)
	if dir == p || dir == "." {
		return p, nil
	}
	rd, err := realPath(dir)
	if err != nil {
		return "", err
	}
	return filepath.Join(rd, base), nil
}

const dataDirHint = "the agent can only read files inside the workspace's data/ folder, by absolute path"

// vetRead returns "" when lit may be read, else the reason it may not.
func (p *pathPolicy) vetRead(lit string) string {
	if lit == "" || hasBadPathChars(lit) {
		return "the file path is empty or contains control characters"
	}
	if urlSchemeRE.MatchString(lit) {
		return p.vetURL(lit)
	}
	if p.dataDir == "" {
		return "file reads are not available here (this file has no workspace data/ folder)"
	}
	if !filepath.IsAbs(lit) {
		return fmt.Sprintf("%q is a relative path; %s: use %s", lit, dataDirHint, filepath.Join(p.dataDir, "<file>"))
	}
	if hasDotDot(lit) {
		return fmt.Sprintf("%q contains \"..\"; %s", lit, dataDirHint)
	}
	glob := strings.IndexAny(lit, "*?[")
	if glob >= 0 {
		return p.vetGlob(lit, glob)
	}
	real, err := realPath(lit)
	if err != nil || !filestore.Within(p.dataDir, real) {
		return fmt.Sprintf("%q is outside the workspace's data/ folder (%s)", lit, p.dataDir)
	}
	return ""
}

// vetGlob accepts a pattern whose literal directory prefix is inside data/ and
// under which no symlink points out of data/ (a glob cannot be resolved one
// match at a time before DuckDB expands it).
func (p *pathPolicy) vetGlob(lit string, glob int) string {
	prefix := lit[:glob]
	if i := strings.LastIndexAny(prefix, `/\`); i >= 0 {
		prefix = prefix[:i+1]
	}
	real, err := realPath(prefix)
	if err != nil || !filestore.Within(p.dataDir, real) {
		return fmt.Sprintf("%q is outside the workspace's data/ folder (%s)", lit, p.dataDir)
	}
	const maxEntries = 20000
	seen := 0
	escaped := false
	walkErr := filepath.WalkDir(real, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		seen++
		if seen > maxEntries {
			return fs.SkipAll
		}
		if d.Type()&fs.ModeSymlink != 0 {
			if target, err := filepath.EvalSymlinks(path); err != nil || !filestore.Within(p.dataDir, target) {
				escaped = true
				return fs.SkipAll
			}
		}
		return nil
	})
	_ = walkErr
	if escaped {
		return fmt.Sprintf("%q matches a symlink that leaves the workspace's data/ folder", lit)
	}
	if seen > maxEntries {
		return fmt.Sprintf("%q matches too many files to check; narrow the pattern", lit)
	}
	return ""
}

func (p *pathPolicy) vetURL(lit string) string {
	// One shared normaliser (internal/storageurl) decides what a plain storage
	// URL is: known scheme, lower case, no query, fragment, percent-escape,
	// backslash, "." / ".." / empty segment. Anything else is never readable.
	if err := storageurl.ValidateRead(lit); err != nil {
		return fmt.Sprintf("the URL %q is not a plain storage URL (%v); only plain object paths inside a storage connection's scope can be read", lit, err)
	}
	for _, s := range p.scopes {
		if storageurl.Within(lit, s) {
			return ""
		}
	}
	if len(p.scopes) == 0 {
		return fmt.Sprintf("the URL %q cannot be read: this workspace has no ready storage connection (s3, gcs, r2 or azure) whose scope contains it", lit)
	}
	return fmt.Sprintf("the URL %q is outside every storage connection's scope (%s)", lit, strings.Join(p.scopes, ", "))
}

// vetWrite returns "" when COPY ... TO lit is allowed: an absolute, glob-free
// path inside data/ whose parent exists and resolves inside data/.
func (p *pathPolicy) vetWrite(lit string) string {
	if lit == "" || hasBadPathChars(lit) || urlSchemeRE.MatchString(lit) {
		return "COPY ... TO may only write to a plain file path inside the workspace's data/ folder"
	}
	if p.dataDir == "" {
		return "file writes are not available here (this file has no workspace data/ folder)"
	}
	if !filepath.IsAbs(lit) || hasDotDot(lit) || strings.ContainsAny(lit, "*?[") {
		return fmt.Sprintf("COPY ... TO %q: the target must be an absolute path inside %s with no \"..\" or wildcards", lit, p.dataDir)
	}
	clean := filepath.Clean(lit)
	// Never write over a database file (an attached connection's file lives in
	// data/ and an overwrite would corrupt it) or something loadable.
	lower := strings.ToLower(clean)
	for _, ext := range []string{".duckdb", ".ducklake", ".db", ".wal", ".sqlite", ".sqlite3", ".duckdb_extension", ".so", ".dylib", ".dll"} {
		if strings.HasSuffix(lower, ext) {
			return fmt.Sprintf("COPY ... TO %q: the agent does not write database or extension files", lit)
		}
	}
	// Nor a table's own metadata: an Iceberg or Delta table names its data files
	// in these, so a crafted one would let iceberg_scan / delta_scan read a file
	// the path rule never saw.
	for _, seg := range strings.FieldsFunc(lower, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == "_delta_log" || strings.HasSuffix(seg, ".avro") || strings.HasSuffix(seg, "metadata.json") || seg == "version-hint.text" {
			return fmt.Sprintf("COPY ... TO %q: the agent does not write the metadata of an Iceberg or Delta table", lit)
		}
	}
	if fi, err := os.Lstat(clean); err == nil && fi.Mode()&fs.ModeSymlink != 0 {
		return fmt.Sprintf("COPY ... TO %q: the target is a symlink", lit)
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(clean))
	if err != nil || !filestore.Within(p.dataDir, parent) {
		return fmt.Sprintf("COPY ... TO %q: the target must be inside %s", lit, p.dataDir)
	}
	// Never into a connection's own files: a DuckLake's catalog or data folder
	// (whatever the file is called), a duckdb or sqlite file.
	target := filepath.Join(parent, filepath.Base(clean))
	for _, prot := range p.protected {
		if filestore.Within(prot, target) {
			return fmt.Sprintf("COPY ... TO %q: the target is part of a connection's own files; the agent does not write there", lit)
		}
	}
	return ""
}

// normalizeScope turns a storage connection's scope into the prefix reads are
// matched against: a plain storage URL that always ends in a slash, so
// "s3://bucket" never admits "s3://bucket-evil/..." and "az://events/2024" never
// admits "az://events/2024-evil/". A scope with any other scheme ("file:///",
// "http://", "hf://") or that is not a plain URL normalises to "": it widens
// nothing.
func normalizeScope(scope string) string {
	sc, err := storageurl.Scope(scope)
	if err != nil {
		return ""
	}
	return sc
}
