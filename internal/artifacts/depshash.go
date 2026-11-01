package artifacts

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"sort"
)

// depsHashTag versions the DepsHash encoding.
const depsHashTag = "slaw-deps-hash/1"

// DepPart is one file a page or view reads without owning it, as its hash
// covers it: a kind ("metric" or "query"), its name and the revision of its
// file ("missing" for one that is not there). The artifacts package does not
// know what a metric is; the workspace, which does, lists the parts.
type DepPart struct {
	Kind string
	Name string
	Rev  string
}

// DepsHash is the hash a page or view is trusted by when its queries call
// metric(): ViewHash or PageHash (base) folded with the revisions of the
// metric files and saved queries it reads, so a git pull that changes what an
// allowed page reads asks again. One canonical, length-prefixed stream:
//
//	string "slaw-deps-hash/1"
//	string base                       (the ViewHash or PageHash, hex)
//	uint64 number of parts
//	then, sorted by kind and name: string kind, string name, string rev
//
// With no parts it returns base unchanged, so a page that does not call
// metric() keeps the hash it was allowed under and every existing decision
// stays valid.
func DepsHash(base string, parts []DepPart) string {
	if len(parts) == 0 {
		return base
	}
	sorted := append([]DepPart(nil), parts...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Kind != sorted[j].Kind {
			return sorted[i].Kind < sorted[j].Kind
		}
		return sorted[i].Name < sorted[j].Name
	})
	h := sha256.New()
	put := func(s string) {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(s)))
		h.Write(n[:])
		h.Write([]byte(s))
	}
	put(depsHashTag)
	put(base)
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(len(sorted)))
	h.Write(n[:])
	for _, p := range sorted {
		put(p.Kind)
		put(p.Name)
		put(p.Rev)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// MetricsLine is the allow prompt's plain line about the metrics a page or
// view reads, shared by Studio and the CLI.
const MetricsLine = "It reads these metrics through their saved queries. Your allow covers their definitions as they are now: if a metric file or its saved query changes, Slaw asks again."
