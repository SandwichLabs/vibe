package bundle

import (
	"fmt"
	"path"
	"path/filepath"

	"github.com/SandwichLabs/slaw/internal/artifacts"
	"github.com/SandwichLabs/slaw/internal/filestore"
)

// Ext is the file extension of a bundle.
const Ext = ".slaw"

// WithExt returns path with ".slaw" appended when it has no extension at all
// (a typed-in name); a path that already has one, whatever it is, is the
// person's own choice and is kept.
func WithExt(path string) string {
	if filepath.Ext(path) == "" {
		return path + Ext
	}
	return path
}

// Counts is how many items of each kind a manifest lists.
type Counts struct {
	Notebooks   int
	Queries     int
	Skills      int
	Artifacts   int
	Semantics   int
	Connections int // 1 when the bundle carries connection definitions, else 0
}

// Counts tallies the manifest's contents by kind. The connection definitions
// travel as one entry, so Connections is 0 or 1.
func (m Manifest) Counts() Counts {
	var c Counts
	for _, e := range m.Contents {
		switch e.Kind {
		case KindNotebook:
			c.Notebooks++
		case KindQuery:
			c.Queries++
		case KindSkill:
			c.Skills++
		case KindArtifact:
			if path.Base(e.Path) == artifacts.ManifestFile {
				c.Artifacts++ // an artifact is two entries; count it once
			}
		case KindSemantics:
			c.Semantics++
		case KindConnections:
			c.Connections = 1
		}
	}
	return c
}

// WriteFile writes an exported bundle's bytes to path, atomically. It does not
// ask about replacing an existing file: the caller's save dialog already did.
func WriteFile(path string, data []byte) error {
	if err := filestore.WriteFileAtomic(path, data, 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	return nil
}
