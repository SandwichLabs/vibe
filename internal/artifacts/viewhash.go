package artifacts

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"sort"
)

// hashTag versions the ViewHash encoding. A different encoding gets a different
// tag, so a stored allow can never match a hash made another way.
const hashTag = "vibe-view-hash/1"

// ViewPart is one view file as hashed: its path relative to the artifact
// folder (forward slashes) and its bytes.
type ViewPart struct {
	Path string
	Data []byte
}

// ViewHash is the sha256 (hex) a custom view is trusted by. It covers the
// manifest (so a pull that changes a query's SQL or connection after an allow
// asks again) and every view file, in one canonical, length-prefixed stream:
//
//	string "vibe-view-hash/1"
//	bytes  artifact.yaml
//	uint64 number of view files
//	then, sorted by path: string path, bytes content
//
// Every string and byte run is prefixed with its length as a big-endian
// uint64, so bytes can never move across a boundary (the end of artifact.yaml
// into the start of a view file) without changing the hash, and a renamed,
// added or removed file changes it. view: script hashes the one view.js; a
// later multi-file view hashes its whole file set with the same function.
//
// Two files with the same path are refused (ErrInvalid), so the hash never
// depends on input order. A per-file role, kind or capability list is not
// hashed here: those come from artifact.yaml, which is entry 0. Adding a
// per-file role to the stream is a new encoding, vibe-view-hash/2.
func ViewHash(manifest []byte, files []ViewPart) (string, error) {
	return hashWithTag(hashTag, manifest, files)
}

func hashWithTag(tag string, manifest []byte, files []ViewPart) (string, error) {
	sorted := append([]ViewPart(nil), files...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	for i := 1; i < len(sorted); i++ {
		if sorted[i].Path == sorted[i-1].Path {
			return "", invalid("two view files named %q", sorted[i].Path)
		}
	}
	h := sha256.New()
	put := func(b []byte) {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(b)))
		h.Write(n[:])
		h.Write(b)
	}
	put([]byte(tag))
	put(manifest)
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(len(sorted)))
	h.Write(n[:])
	for _, f := range sorted {
		put([]byte(f.Path))
		put(f.Data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
