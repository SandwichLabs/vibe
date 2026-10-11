package secrets

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/SandwichLabs/vibe/internal/jsonenc"
)

// Mask replaces a scrubbed value.
const Mask = "***"

// minRedactLen is the shortest literal secret value the redactor will
// replace. Shorter values would mangle ordinary text ("a", "1"); the pattern
// rules below still catch them in key=value position.
const minRedactLen = 4

// secretName matches a credential-shaped name, with any prefix or suffix
// (aws_secret_access_key, s3_secret_access_key, motherduck_token,
// session_token, client_secret, PGPASSWORD). "token" itself and any
// *_token (access_token, bearer_token) count, but not token_count, tokens,
// max_tokens or tokenizer. A bare "key" is deliberately not
// a secret name: "primary key=id" must survive.
const secretName = `(?:\w*(?:password|passwd|pwd|secret|api[_-]?key|access[_-]?key|private[_-]?key|account[_-]?key|connection[_-]?string|credential)\w*|\w*_token|token)`

const quotedVal = `'(?:[^'\\]|\\.|'')*'|"(?:[^"\\]|\\.)*"`

var (
	// name = value, name='value' (Postgres connect errors echo password='...',
	// spike #107; libpq and DuckDB also print password=value).
	assignRE = regexp.MustCompile(`(?i)\b(` + secretName + `)(\s*=\s*)(` + quotedVal + `|[^\s;,)'"]+)`)
	// DDL form: PASSWORD 'x', SECRET 'x', BEARER_TOKEN 'x'.
	ddlRE = regexp.MustCompile(`(?i)\b(` + secretName + `)(\s+)('(?:[^']|'')*')`)
	// "password": "x" (JSON) and password: x (YAML, whole-line).
	jsonRE = regexp.MustCompile(`(?i)("` + secretName + `"\s*:\s*)("(?:[^"\\]|\\.)*")`)
	yamlRE = regexp.MustCompile(`(?im)^(\s*` + secretName + `:[ \t]+)(\S.*)$`)
	// scheme://user:password@host: only the password goes.
	urlRE = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://[^\s:/@]+:)([^\s@/]+)(@)`)
	// Authorization headers: Bearer/Basic only after "Authorization:", or
	// followed by a token-like value (16+ chars, no spaces), so prose such as
	// "basic usage" is left alone.
	authRE   = regexp.MustCompile(`(?i)(authorization\s*[:=]\s*)((?:bearer|basic)\s+)?([^\s,;]+)`)
	bearerRE = regexp.MustCompile(`(?i)\b(bearer|basic)(\s+)([A-Za-z0-9._~+/=-]{16,})`)
)

// RedactPatterns scrubs credential-shaped text only: the readable user='rosa'
// stays. It is what a package without a Redactor of its own uses.
func RedactPatterns(s string) string {
	s = assignRE.ReplaceAllStringFunc(s, func(m string) string {
		sub := assignRE.FindStringSubmatch(m)
		return sub[1] + sub[2] + maskLike(sub[3])
	})
	s = ddlRE.ReplaceAllString(s, "${1}${2}'"+Mask+"'")
	s = jsonRE.ReplaceAllString(s, `${1}"`+Mask+`"`)
	s = yamlRE.ReplaceAllString(s, "${1}"+Mask)
	s = urlRE.ReplaceAllString(s, "${1}"+Mask+"${3}")
	s = authRE.ReplaceAllString(s, "${1}${2}"+Mask)
	s = bearerRE.ReplaceAllString(s, "${1}${2}"+Mask)
	return s
}

// maskLike masks a value keeping its quote style.
func maskLike(v string) string {
	switch {
	case strings.HasPrefix(v, "'"):
		return "'" + Mask + "'"
	case strings.HasPrefix(v, `"`):
		return `"` + Mask + `"`
	}
	return Mask
}

// variants lists the forms a secret can take once quoted for SQL, URLs or
// JSON, so an echoed copy is still found.
func variants(v string) []string {
	out := []string{v, strings.ReplaceAll(v, "'", "''"), strings.ReplaceAll(v, "'", `\'`),
		url.QueryEscape(v), url.PathEscape(v)}
	if b, err := jsonenc.Encode(v, ""); err == nil {
		if j := strings.TrimSpace(string(b)); len(j) >= 2 {
			out = append(out, j[1:len(j)-1])
		}
	}
	return out
}

// Redactor scrubs every secret value it has been told about, plus the
// credential-shaped patterns, from strings. It is safe for concurrent use.
type Redactor struct {
	mu     sync.RWMutex
	values map[string]struct{}
	sorted []string // longest first; rebuilt on Add
}

// NewRedactor returns an empty redactor.
func NewRedactor() *Redactor { return &Redactor{values: map[string]struct{}{}} }

// Add registers secret values to scrub. Short values are ignored (see
// minRedactLen).
func (r *Redactor) Add(values ...string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	changed := false
	for _, orig := range values {
		if len(orig) < minRedactLen {
			continue
		}
		for _, v := range variants(orig) {
			if len(v) < minRedactLen {
				continue
			}
			if _, ok := r.values[v]; ok {
				continue
			}
			r.values[v] = struct{}{}
			changed = true
		}
	}
	if changed {
		r.sorted = r.sorted[:0]
		for v := range r.values {
			r.sorted = append(r.sorted, v)
		}
		sort.Slice(r.sorted, func(i, j int) bool { return len(r.sorted[i]) > len(r.sorted[j]) })
	}
}

// AddCredential registers a credential's secret fields.
func (r *Redactor) AddCredential(c Credential) { r.Add(c.SecretValues()...) }

// Redact scrubs known values first (so a password inside a longer string is
// found), then the patterns. A nil Redactor applies the patterns only.
func (r *Redactor) Redact(s string) string {
	if r != nil {
		r.mu.RLock()
		for _, v := range r.sorted {
			if strings.Contains(s, v) {
				s = strings.ReplaceAll(s, v, Mask)
			}
		}
		r.mu.RUnlock()
	}
	return RedactPatterns(s)
}

// RedactError returns err's text scrubbed, or "" for nil.
func (r *Redactor) RedactError(err error) string {
	if err == nil {
		return ""
	}
	return r.Redact(err.Error())
}

// NewRedactingHandler wraps a slog.Handler so messages and attributes are
// scrubbed before they are written.
func NewRedactingHandler(next slog.Handler, r *Redactor) slog.Handler {
	return &redactHandler{next: next, r: r}
}

type redactHandler struct {
	next slog.Handler
	r    *Redactor
}

func (h *redactHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

func (h *redactHandler) Handle(ctx context.Context, rec slog.Record) error {
	out := slog.NewRecord(rec.Time, rec.Level, h.r.Redact(rec.Message), rec.PC)
	rec.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(h.attr(a))
		return true
	})
	return h.next.Handle(ctx, out)
}

func (h *redactHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	red := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		red[i] = h.attr(a)
	}
	return &redactHandler{next: h.next.WithAttrs(red), r: h.r}
}

func (h *redactHandler) WithGroup(name string) slog.Handler {
	return &redactHandler{next: h.next.WithGroup(name), r: h.r}
}

func (h *redactHandler) attr(a slog.Attr) slog.Attr {
	a.Value = a.Value.Resolve()
	switch a.Value.Kind() {
	case slog.KindString:
		a.Value = slog.StringValue(h.r.Redact(a.Value.String()))
	case slog.KindGroup:
		g := a.Value.Group()
		out := make([]slog.Attr, len(g))
		for i, ga := range g {
			out[i] = h.attr(ga)
		}
		a.Value = slog.GroupValue(out...)
	case slog.KindAny:
		if b, ok := a.Value.Any().([]byte); ok {
			a.Value = slog.StringValue(h.r.Redact(string(b)))
			break
		}
		// Errors and arbitrary values print through their text; only replace
		// when something was scrubbed so structured values stay structured.
		text := fmt.Sprintf("%v", a.Value.Any())
		if red := h.r.Redact(text); red != text {
			a.Value = slog.StringValue(red)
		}
	}
	return a
}

var (
	identityAssignRE = regexp.MustCompile(`(?i)\b(user(?:name)?\s*=\s*)('[^']*'|"[^"]*"|[^\s'",;)]+)`)
	identityForRE    = regexp.MustCompile(`(?i)(\bfor user\s+)("[^"]*"|'[^']*')`)
)

// RedactIdentity also hides the user name in `user='rosa'` and `for user
// "rosa"` (what DuckDB and Postgres echo). The UI may show it to the person
// who owns the connection; text that reaches the agent or an MCP client never
// carries it.
func RedactIdentity(s string) string {
	s = identityAssignRE.ReplaceAllString(s, "${1}'"+Mask+"'")
	return identityForRE.ReplaceAllString(s, "${1}\""+Mask+"\"")
}

var secretNameRE = regexp.MustCompile(`(?i)^(?:` + secretName + `|\w*_pass|pass|\w*_?url|\w*dsn)$`)

// IsSecretName reports whether an environment-variable or option name looks
// like it holds a credential: the redactor's own secret-name rule, plus the
// common PASS, URL and DSN forms (DATABASE_URL often embeds a password).
func IsSecretName(name string) bool { return secretNameRE.MatchString(name) }
