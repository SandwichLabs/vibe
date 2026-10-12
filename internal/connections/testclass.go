package connections

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/SandwichLabs/vibe/internal/session"
)

// Failure codes of a connection test (TestResult.Code). They are what a UI
// switches on; Message is the plain-language line for the same failure.
const (
	CodeError            = "error"
	CodeInvalid          = "invalid"
	CodeServerChanged    = "server-changed"
	CodeExtensionMissing = "extension-missing"
	CodeTimeout          = "timeout"
	CodeBadSignin        = "bad-signin"
	CodeDBNotFound       = "db-not-found"
	CodeSSLRequired      = "ssl-required"
	CodeKeychain         = "keychain"
	CodeUnreachable      = "unreachable"
)

// ServerChangedMessage is the plain line shown when a test is refused because
// the definition points at a different server than the saved sign-in.
const ServerChangedMessage = "The server changed, so your saved sign-in won't be used. Enter it again to test."

var reHostFor = regexp.MustCompile(`host='([^']+)'`)

// Failure is what Classify needs to word one failed attempt to connect.
type Failure struct {
	// Raw is the redacted driver text. DuckDB and the database drivers return
	// plain errors, so this is the one place their wording is read.
	Raw string
	// Status is the status the attempt ended in (may be empty).
	Status session.StatusKind
	// InstallExtension names a missing extension the person can install.
	InstallExtension string
	// TimedOut is set by the code that hit the deadline, so a timeout does not
	// depend on any message text.
	TimedOut bool
	// Def is the definition tried (Type, Host and Database word the message).
	Def Def
}

// Classify maps a failed attempt to a code and a plain-language message (SPEC
// 06). It is the single classifier: Test fills TestResult.Code and Message with
// it where the error is produced, and Studio's rail uses it for session errors.
func Classify(f Failure) (code, message string) {
	raw := f.Raw
	low := strings.ToLower(raw)
	host := f.Def.Host
	if host == "" {
		if m := reHostFor.FindStringSubmatch(raw); m != nil {
			host = m[1]
		}
	}
	if host == "" {
		host = "the server"
	}
	code, message = CodeError, "Vibe couldn't connect with these settings."
	switch {
	case f.InstallExtension != "" || f.Status == session.StatusExtensionMissing:
		ext := f.InstallExtension
		if ext == "" {
			ext = "missing"
		}
		code = CodeExtensionMissing
		message = fmt.Sprintf("Vibe's database engine needs the %s extension, which isn't installed yet. Install it, then test again.", ext)
	case f.TimedOut || strings.Contains(low, "timed out"):
		code = CodeTimeout
		message = "No answer after 10 seconds. Check the address and whether you need a VPN."
	case strings.Contains(low, "password authentication failed") || strings.Contains(low, "authentication failed") ||
		strings.Contains(low, "invalid token") || strings.Contains(low, "not authenticated") || f.Status == session.StatusNeedsCredentials:
		code = CodeBadSignin
		message = fmt.Sprintf("The server turned down the username or password. Check them and try again. If you're sure they're right, ask whoever runs the database whether your account can reach %s.", host)
		if t := f.Def.Type; t == "motherduck" || strings.HasPrefix(t, "s3") || t == "gcs" || t == "r2" || t == "azure" {
			message = "The service turned down these keys or this token. Check them and try again."
		}
	case strings.Contains(low, "does not exist") && strings.Contains(low, "database"):
		db := f.Def.Database
		if db == "" {
			db = "that"
		}
		code = CodeDBNotFound
		message = fmt.Sprintf("The server answered, but it has no database called %s.", db)
	case strings.Contains(low, "ssl") && (strings.Contains(low, "require") || strings.Contains(low, "not supported")):
		code = CodeSSLRequired
		message = "The server needs an encrypted connection. Open Advanced options and set SSL mode to require."
	case strings.Contains(low, "keychain"):
		code = CodeKeychain
		message = "Vibe can't reach your keychain. Use environment variables for this connection instead."
	case strings.Contains(low, "could not connect") || strings.Contains(low, "connection refused") || strings.Contains(low, "no such host") ||
		strings.Contains(low, "could not translate host") || strings.Contains(low, "network is unreachable") || strings.Contains(low, "unable to connect"):
		code = CodeUnreachable
		message = fmt.Sprintf("Vibe couldn't reach %s. Is the VPN on?", host)
	}
	return code, message
}
