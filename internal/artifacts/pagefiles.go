package artifacts

import (
	"bytes"
	"path"
	"sort"
	"strings"
	"unicode/utf8"
)

// Caps on a page's file set (H1). The byte caps apply before any parse.
const (
	MaxPageFiles       = 200
	MaxPageBytes       = 5 << 20
	MaxPageFileBytes   = 1 << 20
	MaxTemplateBytes   = 256 << 10
	maxPageSegmentLen  = 100
	maxPagePathLen     = 200
	assetsDir          = "assets"
	reservedVibeDir    = "_vibe"
	reservedVibeLibDir = "vibe-lib"
)

// Roles a page file can have; the role is part of PageHash.
const (
	RoleEntry   = "entry"
	RolePartial = "partial"
	RoleScript  = "script"
	RoleStyle   = "style"
	RoleImage   = "image"
	RoleFont    = "font"
	RoleData    = "data"
)

// PageFile is one file under html/: its path relative to html/ (forward
// slashes) and its bytes.
type PageFile struct {
	Path string
	Data []byte
}

// extRoles is the closed extension list, with each extension's role (an entry
// template is told from a partial by the manifest).
var extRoles = map[string]string{
	".tmpl": RolePartial, ".js": RoleScript, ".mjs": RoleScript, ".css": RoleStyle,
	".svg": RoleImage, ".png": RoleImage, ".jpg": RoleImage, ".webp": RoleImage,
	".woff2": RoleFont, ".json": RoleData,
}

// pageSegment is one path segment: no dotfile, no leading dash, a short plain
// name.
func validPageSegment(s string) bool {
	if s == "" || len(s) > maxPageSegmentLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || (i > 0 && (c == '.' || c == '-'))
		if !ok {
			return false
		}
	}
	return true
}

// checkPageFilePath applies H1's layout rules to one path: files directly in
// html/, in html/assets/, or in html/assets/<dir>/; .tmpl files only directly
// in html/; the extension from the closed list; nothing named like the host's
// reserved folders. It returns the role.
func checkPageFilePath(p string) (string, error) {
	if len(p) > maxPagePathLen || p != path.Clean(p) || strings.HasPrefix(p, "/") {
		return "", invalid("html/%s is not a plain relative path", truncateForError(p))
	}
	segs := strings.Split(p, "/")
	for _, s := range segs {
		if !validPageSegment(s) {
			return "", invalid("html/%s: %q is not a usable name (letters, digits, . _ -; no dotfiles)", truncateForError(p), s)
		}
	}
	if segs[0] == reservedVibeDir || segs[0] == reservedVibeLibDir {
		return "", invalid("html/%s: %s is reserved for the host", p, segs[0])
	}
	if len(segs) > 1 && segs[0] != assetsDir {
		return "", invalid("html/%s: only %s/ may hold folders", p, assetsDir)
	}
	if len(segs) > 3 {
		return "", invalid("html/%s is nested too deep (at most %s/<folder>/<file>)", p, assetsDir)
	}
	role, ok := extRoles[path.Ext(p)]
	if !ok {
		return "", invalid("html/%s: files may end in .tmpl .js .mjs .css .svg .png .jpg .webp .woff2 or .json", p)
	}
	if ext := path.Ext(p); len(segs) == 1 && ext != ".tmpl" && ext != ".js" && ext != ".mjs" {
		return "", invalid("html/%s: only .tmpl, .js and .mjs files go directly under html/; put it in %s/ (nothing else there could be reached)", p, assetsDir)
	}
	if path.Ext(p) == ".tmpl" && len(segs) > 1 {
		return "", invalid("html/%s: templates belong directly under html/", p)
	}
	return role, nil
}

func truncateForError(s string) string {
	if len(s) > 60 {
		return s[:60] + "..."
	}
	return s
}

// checkPageFiles applies the caps and the per-file rules to a file set and
// returns each file's role (the entry told from the partials). Files must not
// repeat a path, not even in different letter case.
func checkPageFiles(entry string, files []PageFile) (map[string]string, error) {
	if len(files) == 0 {
		return nil, invalid("a view: html artifact needs %s/%s", HTMLDir, entry)
	}
	if len(files) > MaxPageFiles {
		return nil, invalid("%d files under %s/, over the limit of %d", len(files), HTMLDir, MaxPageFiles)
	}
	roles := make(map[string]string, len(files))
	folded := make(map[string]bool, len(files))
	total := 0
	for _, f := range files {
		role, err := checkPageFilePath(f.Path)
		if err != nil {
			return nil, err
		}
		if folded[strings.ToLower(f.Path)] {
			return nil, invalid("html/%s repeats a path (paths are compared without regard to case)", f.Path)
		}
		folded[strings.ToLower(f.Path)] = true
		if err := checkPageFileSize(f); err != nil {
			return nil, err
		}
		total += len(f.Data)
		if f.Path == entry {
			role = RoleEntry
		}
		roles[f.Path] = role
	}
	if total > MaxPageBytes {
		return nil, invalid("the files under %s/ total %d bytes, over the %d byte limit", HTMLDir, total, MaxPageBytes)
	}
	if roles[entry] != RoleEntry {
		return nil, invalid("the entry template %s/%s does not exist", HTMLDir, entry)
	}
	return roles, nil
}

func checkPageFileSize(f PageFile) error {
	limit := MaxPageFileBytes
	if path.Ext(f.Path) == ".tmpl" {
		limit = MaxTemplateBytes
	}
	if len(f.Data) > limit {
		return invalid("html/%s is %d bytes, over the %d byte limit", f.Path, len(f.Data), limit)
	}
	return nil
}

// sortedFiles returns a copy sorted by path.
func sortedFiles(files []PageFile) []PageFile {
	out := append([]PageFile(nil), files...)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// checkPageContent applies the per-extension content rules: text files are
// UTF-8 without hidden characters, data is JSON, images and fonts start with
// their format's magic bytes (so a page cannot pass HTML off as a picture),
// stylesheets and SVG are scanned for active or external content.
func checkPageContent(f PageFile) error {
	where := "html/" + f.Path
	switch path.Ext(f.Path) {
	case ".tmpl", ".js", ".mjs", ".css":
		if err := checkPageText(where, f.Data); err != nil {
			return err
		}
		if path.Ext(f.Path) == ".css" {
			if err := checkCSS(string(f.Data), false); err != nil {
				return invalid("%s: %v", where, err)
			}
		}
	case ".json":
		if err := checkPageText(where, f.Data); err != nil {
			return err
		}
		if err := decodeSingleJSON(f.Data); err != nil {
			return invalid("%s: %v", where, err)
		}
	case ".svg":
		if err := checkPageText(where, f.Data); err != nil {
			return err
		}
		if err := checkSVG(f.Data); err != nil {
			return invalid("%s: %v", where, err)
		}
	default:
		if !hasMagic(path.Ext(f.Path), f.Data) {
			return invalid("%s is not a real %s file", where, strings.TrimPrefix(path.Ext(f.Path), "."))
		}
	}
	return nil
}

func checkPageText(where string, data []byte) error {
	if !utf8.Valid(data) {
		return invalid("%s must be valid UTF-8 text", where)
	}
	if r, ok := firstHiddenRune(string(data), true); ok {
		return invalid("%s contains a control or invisible character (U+%04X), which could make the code differ from what is shown", where, r)
	}
	if bytes.ContainsRune(data, 0) || bytes.ContainsRune(data, markOpen) || bytes.ContainsRune(data, markClose) {
		return invalid("%s contains a reserved character (NUL or U+E000/U+E001)", where)
	}
	return nil
}

func hasMagic(ext string, data []byte) bool {
	switch ext {
	case ".png":
		return bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n"))
	case ".jpg":
		return bytes.HasPrefix(data, []byte{0xff, 0xd8, 0xff})
	case ".webp":
		return len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP"))
	case ".woff2":
		return bytes.HasPrefix(data, []byte("wOF2"))
	}
	return false
}

// IgnoredPageFile reports a file name under html/ that the file set neither
// hashes, serves nor refuses: the clutter a file manager leaves behind. Every
// other dotfile is refused.
func IgnoredPageFile(name string) bool { return name == ".DS_Store" || name == "Thumbs.db" }
