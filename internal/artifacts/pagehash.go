package artifacts

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"path"
	"sort"
)

// pageHashTag versions the PageHash encoding.
const pageHashTag = "assay-page-hash/1"

// PageHash is the sha256 (hex) an HTML page is trusted by: ViewHash
// generalised to a file set with a role per file. One canonical,
// length-prefixed stream:
//
//	string "assay-page-hash/1"
//	bytes  artifact.yaml                       (entry 0: a pull that changes a
//	                                            query's SQL or connection asks again)
//	string derived kind ("static" or "scripted")
//	uint64 number of declared capabilities, then each name, sorted
//	uint64 number of files
//	then, sorted by path: string path, bytes content, string role
//
// Every string and byte run is prefixed with its length as a big-endian
// uint64, so a byte moved across a boundary (the end of one file into the
// start of the next), a renamed, added, removed or moved file, a changed role
// and a changed kind or capability set all change the hash. The page's host
// is therefore part of what the person allowed: a page cannot switch from
// static to scripted, or add a capability, without asking again.
//
// ViewHash is a different encoding with a different tag and is unchanged:
// view: script artifacts keep the hashes they were allowed under.
func PageHash(manifest []byte, files []PageFile, info *PageInfo) (string, error) {
	sorted := sortedFiles(files)
	for i := 1; i < len(sorted); i++ {
		if sorted[i].Path == sorted[i-1].Path {
			return "", invalid("two page files named %q", sorted[i].Path)
		}
	}
	h := sha256.New()
	put := func(b []byte) {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(b)))
		h.Write(n[:])
		h.Write(b)
	}
	putCount := func(n int) {
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], uint64(n))
		h.Write(b[:])
	}
	put([]byte(pageHashTag))
	put(manifest)
	put([]byte(info.Kind))
	caps := append([]string(nil), info.Capabilities...)
	sort.Strings(caps)
	putCount(len(caps))
	for _, c := range caps {
		put([]byte(c))
	}
	putCount(len(sorted))
	for _, f := range sorted {
		put([]byte(f.Path))
		put(f.Data)
		put([]byte(pageRole(f.Path, info.Entry)))
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// pageRole is a file's role in the hash: the entry template, a partial, a
// script, a stylesheet, an image, a font or data.
func pageRole(p, entry string) string {
	if p == entry {
		return RoleEntry
	}
	if r, ok := extRoles[path.Ext(p)]; ok {
		return r
	}
	return "unknown"
}
