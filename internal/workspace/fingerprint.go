package workspace

import (
	"crypto/sha256"
	"encoding/hex"
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
	for _, part := range []string{
		"vibe-conn-v1",
		strings.ToLower(strings.TrimSpace(d.Type)),
		strings.ToLower(strings.TrimSpace(d.Host)),
		strconv.Itoa(d.Port),
		strings.TrimSpace(d.Database),
		strings.TrimSpace(d.Scope),
	} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}
