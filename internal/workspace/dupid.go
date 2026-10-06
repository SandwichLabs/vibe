package workspace

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

// Known is the registry of workspaces this person has opened: path -> id. The
// caller (Studio's recent list in prefs) supplies it.
type Known map[string]string

// DuplicateIDs reports the other live paths in known that carry the same id as
// the workspace at path. A `cp -r` of a workspace copies its vibe.yaml and so
// its id, which would make both copies share keychain entries; the caller offers
// "Regenerate id" for the copy. "Live" means the folder still has a vibe.yaml
// (a registry entry for a deleted folder is not a duplicate). The result is
// sorted and never includes path itself.
func DuplicateIDs(path, id string, known Known) []string {
	self := canonical(path)
	var out []string
	for p, other := range known {
		if other != id || canonical(p) == self {
			continue
		}
		if fi, err := os.Lstat(filepath.Join(p, ConfigFile)); err != nil || !fi.Mode().IsRegular() {
			continue
		}
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func canonical(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real
	}
	return filepath.Clean(p)
}

var idLineRE = regexp.MustCompile(`(?m)^id:[ \t]*.*$`)

// RegenerateID gives the workspace at root a fresh id, editing only the id
// line of vibe.yaml so comments and formatting survive. It returns the new id.
// Credentials saved under the old id stay with the original workspace; the
// copy starts without them.
func RegenerateID(root string) (string, error) {
	return regenerateID(root)
}
