package artifacts

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
)

// filePart is one of the artifact's two files as read: its bytes and whether
// it exists. An absent file and an empty one are different states.
type filePart struct {
	data    []byte
	present bool
}

// revision is the one revision of an artifact: a hash over both files, each
// length-prefixed (and flagged present or absent) so that bytes moving from the
// end of one file to the start of the other change it. Zero when neither file
// exists, which is the revision of an artifact that is not there.
func revision(manifest, view filePart) string {
	if !manifest.present && !view.present {
		return ""
	}
	h := sha256.New()
	for _, p := range []filePart{manifest, view} {
		if !p.present {
			h.Write([]byte{0})
			continue
		}
		h.Write([]byte{1})
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(p.data)))
		h.Write(n[:])
		h.Write(p.data)
	}
	return hex.EncodeToString(h.Sum(nil)[:16])
}

// RevOf is Rev for two files that both exist.
func RevOf(manifest, view []byte) string {
	return revision(filePart{manifest, true}, filePart{view, true})
}

// revisionWithFiles is the one revision of a view: html artifact: the pair
// revision (the view part is absent for such an artifact) extended over every
// file under html/, each length-prefixed (path, then bytes) in path order, so
// a change to any one file, a renamed, added or removed file, or a byte moved
// across a file boundary changes it. files == nil means the artifact has no
// page files and the pair revision is returned unchanged, which keeps every
// existing revision stable.
func revisionWithFiles(manifest, view filePart, files []PageFile) string {
	base := revision(manifest, view)
	if files == nil {
		return base
	}
	h := sha256.New()
	put := func(b []byte) {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(b)))
		h.Write(n[:])
		h.Write(b)
	}
	put([]byte("vibe-page-rev/1"))
	put([]byte(base))
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(len(files)))
	h.Write(n[:])
	for _, f := range sortedFiles(files) {
		put([]byte(f.Path))
		put(f.Data)
	}
	return hex.EncodeToString(h.Sum(nil)[:16])
}

// pageRevOf is the revision of a prepared artifact as the stores compute it
// for what is on disk (for view: html, no view file exists).
func pageRevOf(p prepared) string {
	if p.files != nil {
		return revisionWithFiles(filePart{p.manifest, true}, filePart{}, p.files)
	}
	return RevOf(p.manifest, p.view)
}
