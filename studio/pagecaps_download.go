package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/SandwichLabs/vibe/internal/filestore"
	"github.com/SandwichLabs/vibe/internal/pagehost"
)

// maxDownloadBytes is the largest file a page may save (H5).
const maxDownloadBytes = 20 << 20

type downloadKind struct {
	ext   string
	magic []byte
}

// downloadTypes is the closed set a page may save, by the type it names.
var downloadTypes = map[string]downloadKind{
	"text/csv": {ext: ".csv"}, "csv": {ext: ".csv"},
	"application/json": {ext: ".json"}, "json": {ext: ".json"},
	"image/svg+xml": {ext: ".svg"}, "svg": {ext: ".svg"},
	"image/png": {ext: ".png", magic: []byte("\x89PNG\r\n\x1a\n")}, "png": {ext: ".png", magic: []byte("\x89PNG\r\n\x1a\n")},
	"application/pdf": {ext: ".pdf", magic: []byte("%PDF-")}, "pdf": {ext: ".pdf", magic: []byte("%PDF-")},
	"text/plain": {ext: ".txt"}, "text": {ext: ".txt"}, "txt": {ext: ".txt"},
}

type downloadArg struct {
	Filename string `json:"filename"`
	Type     string `json:"type"`
	Enc      string `json:"enc"`
	Data     string `json:"data"`
}

// sanitizeFilename makes the name the dialog is filled with: no path parts, no
// control, format or bidi characters, no characters a file system refuses, no
// leading dot or space, a bounded length, and the extension of the type.
func sanitizeFilename(name, ext string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		switch {
		case r == utf8.RuneError, unicode.IsControl(r), unicode.Is(unicode.Cf, r), unicode.Is(unicode.Zl, r), unicode.Is(unicode.Zp, r):
			return -1
		case strings.ContainsRune(`<>:"|?*`, r):
			return '_'
		}
		return r
	}, name)
	name = strings.Trim(name, ". \t")
	if low := strings.ToLower(name); strings.HasSuffix(low, ext) {
		name = name[:len(name)-len(ext)]
	}
	for len(name) > 120 {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	name = strings.Trim(name, ". \t")
	if name == "" || isReservedName(name) {
		name = "download"
	}
	return name + ext
}

// isReservedName is a Windows device name, which is reserved with any
// extension ("con.txt" too): CON, PRN, AUX, NUL, COM1-9, LPT1-9 (and the
// superscript digits Windows also reserves).
func isReservedName(n string) bool {
	base := n
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	base = strings.ToUpper(strings.TrimRight(base, " "))
	switch base {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return true
	}
	if len(base) >= 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) {
		switch strings.TrimPrefix(strings.TrimPrefix(base, "COM"), "LPT") {
		case "1", "2", "3", "4", "5", "6", "7", "8", "9", "\u00b9", "\u00b2", "\u00b3":
			return true
		}
	}
	return false
}

// decodeDownload checks the page's argument and returns the file's bytes, the
// extension and the clean name.
func decodeDownload(raw []byte) (data []byte, filename, ext string, code string) {
	var a downloadArg
	if !decodeArg(raw, &a) {
		return nil, "", "", pagehost.CodeRefused
	}
	kind, ok := downloadTypes[strings.ToLower(strings.TrimSpace(a.Type))]
	if !ok {
		return nil, "", "", pagehost.CodeRefused
	}
	switch a.Enc {
	case "text":
		data = []byte(a.Data)
	case "base64":
		if base64.StdEncoding.DecodedLen(len(a.Data)) > maxDownloadBytes+3 {
			return nil, "", "", pagehost.CodeTooLarge
		}
		b, err := base64.StdEncoding.DecodeString(a.Data)
		if err != nil {
			return nil, "", "", pagehost.CodeRefused
		}
		data = b
	default:
		return nil, "", "", pagehost.CodeRefused
	}
	if len(data) > maxDownloadBytes {
		return nil, "", "", pagehost.CodeTooLarge
	}
	if kind.magic != nil && !bytes.HasPrefix(data, kind.magic) {
		return nil, "", "", pagehost.CodeRefused
	}
	return data, sanitizeFilename(a.Filename, kind.ext), kind.ext, ""
}

// callDownload is save: the person picks where in the native dialog, with the
// name filled in, and cancelling writes nothing. Studio writes only the path
// the person chose, and only if the page is still the one they allowed.
func (w *pageWindow) callDownload(op string, raw []byte) ([]byte, string) {
	if op != "save" || w.svc.save == nil {
		return nil, pagehost.CodeUnavailable
	}
	data, filename, ext, code := decodeDownload(raw)
	if code != "" {
		return nil, code
	}
	th := w.host.saveThrottleFor(w.name)
	if !th.allow(time.Now()) {
		return nil, pagehost.CodeLimit
	}
	ctx, cancel := context.WithTimeout(w.ctx, 30*time.Minute)
	defer cancel()
	path, err := w.svc.save(ctx, windowTitle(&w.page.Manifest, w.name), filename, ext)
	if err != nil {
		if w.host.logger != nil {
			w.host.logger.Debug("page save dialog failed", "artifact", w.name, "error", err)
		}
		if err == errSaveBusy {
			return nil, pagehost.CodeLimit
		}
		return nil, pagehost.CodeUnavailable
	}
	if path == "" {
		th.cancel(time.Now())
		return []byte(`{"saved":false}`), ""
	}
	// The person may have taken minutes: ask the decision itself, not the cache.
	if w.ctx.Err() != nil || !w.allowed(w.hash) {
		return nil, pagehost.CodeRefused
	}
	if err := writeChosenFile(path, data); err != nil {
		if w.host.logger != nil {
			w.host.logger.Debug("page save failed", "artifact", w.name, "error", err)
		}
		return nil, pagehost.CodeUnavailable
	}
	return []byte(`{"saved":true}`), ""
}

// writeChosenFile writes to the path the person picked in the dialog.
func writeChosenFile(path string, data []byte) error {
	return filestore.WriteFileAtomic(path, data, os.FileMode(0o644))
}
