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
