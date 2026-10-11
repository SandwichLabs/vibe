package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
)

// Fingerprint identifies the server a connection points at: its type, host,
// port and database or bucket scope (plan §4.2, design #106). A keychain entry
// is bound to it, so when a pull changes any of those the connection becomes
// needs-credentials ("the server changed since you signed in") instead of
// sending a saved password to a different server. Compute it on the
// environment-resolved definition, so a changed variable counts too.
//
// It is a one-way hash of non-secret values; it is safe to store and show.
func Fingerprint(d ConnectionDef) string {
	h := sha256.New()
	// Everything that decides which server or file the credential reaches:
	// path (file connections), region and every option (endpoint, url_style,
	// hostaddr, sslmode, account, ...). Erring towards inclusion is the safe
	// side: a spurious change only asks the person to sign in again.
	keys := make([]string, 0, len(d.Options))
	for k, v := range d.Options {
		if strings.TrimSpace(v) == "" {
			continue // an empty option is the same as none: never prompts
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, 8+len(keys))
	parts = append(parts,
		"vibe-conn-v2",
		strings.ToLower(strings.TrimSpace(d.Type)),
		strings.ToLower(strings.TrimSpace(d.Host)),
		strconv.Itoa(d.Port),
		strings.TrimSpace(d.Database),
		strings.TrimSpace(d.Scope),
		strings.TrimSpace(d.Path),
		strings.ToLower(strings.TrimSpace(d.Region)),
	)
	for _, k := range keys {
		parts = append(parts, "opt:"+strings.ToLower(k)+"="+strings.TrimSpace(d.Options[k]))
	}
	for _, part := range parts {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}
