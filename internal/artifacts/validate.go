package artifacts

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/SandwichLabs/vibe/internal/a2ui"
	"github.com/SandwichLabs/vibe/internal/connname"
	"github.com/SandwichLabs/vibe/internal/engine"
)

// nameRe is the shape of a parameter or query name: it starts with a letter
// (so a positional $1 can never be a declared parameter) and the rest is what
// engine.ValidateParamName allows.
var nameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

func validName(kind, name string) error {
	if len(name) > MaxNameLength || !nameRe.MatchString(name) {
		return invalid("%s name %q must start with a letter and use only letters, digits and underscores (at most %d characters)", kind, name, MaxNameLength)
	}
	return engine.ValidateParamName(name)
}

// Validate checks a manifest's structure: names, types, defaults, caps, that
// every $name a query uses is declared, and the options_query links. It fills
// each Query's Uses and the default view kind. It never looks at what the SQL
// means: that is the agent SQL policy's job (internal/agents). It returns the
// first problem ValidateAll finds.
func Validate(m *Manifest) error {
	if errs := ValidateAll(m); len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// ValidateAll is Validate without stopping at the first problem: one error per
// failing check (the header's checks, each parameter, each query, each
// options_query link), in the order Validate would meet them, so a caller can
// show the whole list at once. Validate's error is always the first of these.
func ValidateAll(m *Manifest) []error {
	errs := validateHeader(m)
	declared, perr := validateParams(m)
	errs = append(errs, perr...)
	queries, qerr := validateQueries(m, declared)
	errs = append(errs, qerr...)
	return append(errs, validateOptionsQueries(m, declared, queries)...)
}

// validateHeader collects the header's independent checks. The view kind and
// the html block come first because the default view kind is filled here.
func validateHeader(m *Manifest) []error {
	var errs []error
	add := func(err error) {
		if err != nil {
			errs = append(errs, err)
		}
	}
	if m.Version != Version {
		add(invalid("version must be %d", Version))
	}
	add(validateTitle(m))
	if m.View == "" {
		m.View = ViewA2UI
	}
	if m.View != ViewA2UI && m.View != ViewScript && m.View != ViewHTML {
		add(invalid("view must be %q, %q or %q", ViewA2UI, ViewScript, ViewHTML))
	}
	add(validateHTMLSpec(m))
	switch {
	case len(m.Queries) == 0:
		add(invalid("an artifact needs at least one query"))
	case len(m.Queries) > MaxQueries:
		add(invalid("%d queries, over the limit of %d", len(m.Queries), MaxQueries))
	}
	if len(m.Parameters) > MaxParams {
		add(invalid("%d parameters, over the limit of %d", len(m.Parameters), MaxParams))
	}
	return errs
}

func validateTitle(m *Manifest) error {
	title := strings.TrimSpace(m.Title)
	if title == "" || utf8.RuneCountInString(title) > MaxTitleLength || strings.ContainsAny(title, "\n\r\x00") {
		return invalid("title is required: one line, at most %d characters", MaxTitleLength)
	}
	if !utf8.ValidString(title) || !utf8.ValidString(m.Description) {
		return invalid("title and description must be valid UTF-8 text")
	}
	if utf8.RuneCountInString(m.Description) > MaxDescription || strings.ContainsRune(m.Description, 0) {
		return invalid("description is over %d characters", MaxDescription)
	}
	return nil
}

func validateParams(m *Manifest) (map[string]*Param, []error) {
	declared := map[string]*Param{}
	folded := map[string]bool{}
	var errs []error
	for i := range m.Parameters {
		p := &m.Parameters[i]
		if err := validName("parameter", p.Name); err != nil {
			errs = append(errs, err)
			continue
		}
		if folded[strings.ToLower(p.Name)] {
			errs = append(errs, invalid("parameter %q is declared twice (names are compared without regard to case)", p.Name))
			continue
		}
		folded[strings.ToLower(p.Name)] = true
		declared[p.Name] = p
		if err := validateParam(p); err != nil {
			errs = append(errs, err)
		}
	}
	return declared, errs
}

// fixedNow is the clock the structural check resolves date defaults on: only
// whether they resolve matters here, not to what.
func fixedNow() time.Time { return time.Date(2026, time.January, 15, 12, 0, 0, 0, time.UTC) }

func validateParam(p *Param) error {
	if utf8.RuneCountInString(p.Label) > MaxTitleLength || strings.ContainsAny(p.Label, "\n\r\x00") {
		return invalid("parameter %q: label is one line, at most %d characters", p.Name, MaxTitleLength)
	}
	if !utf8.ValidString(p.Label) {
		return invalid("parameter %q: label must be valid UTF-8 text", p.Name)
	}
	if err := validateParamFields(p); err != nil {
		return err
	}
	if p.Default == nil {
		return invalid("parameter %q needs a default", p.Name)
	}
	if _, err := defaultValue(p, fixedNow()); err != nil {
		return invalid("%v", err)
	}
	return nil
}

// validateParamFields refuses a constraint that does not belong to the type
// and checks the ones that do.
func validateParamFields(p *Param) error {
	numeric := p.Type == TypeNumber || p.Type == TypeInteger
	switch p.Type {
	case TypeText, TypeNumber, TypeInteger, TypeDate, TypeBoolean, TypeEnum:
	default:
		return invalid("parameter %q: type %q is not one of text, number, integer, date, boolean, enum", p.Name, p.Type)
	}
	if p.MaxLength != 0 && (p.Type != TypeText || p.MaxLength < 0 || p.MaxLength > MaxTextLength) {
		return invalid("parameter %q: max_length is for text only, 1 to %d", p.Name, MaxTextLength)
	}
	if (p.Min != nil || p.Max != nil) && !numeric {
		return invalid("parameter %q: min and max are for number and integer only", p.Name)
	}
	for _, b := range []*float64{p.Min, p.Max} {
		if b != nil && (math.IsNaN(*b) || math.IsInf(*b, 0)) {
			return invalid("parameter %q: min and max must be finite numbers", p.Name)
		}
	}
	if p.Min != nil && p.Max != nil && *p.Min > *p.Max {
		return invalid("parameter %q: min is above max", p.Name)
	}
	if (len(p.Options) > 0 || p.OptionsQuery != "") && p.Type != TypeEnum {
		return invalid("parameter %q: options and options_query are for enum only", p.Name)
	}
	if p.Type == TypeEnum {
		return validateEnum(p)
	}
	return nil
}

func validateEnum(p *Param) error {
	if (len(p.Options) > 0) == (p.OptionsQuery != "") {
		return invalid("enum parameter %q needs exactly one of options or options_query", p.Name)
	}
	if len(p.Options) > MaxEnumOptions {
		return invalid("enum parameter %q has %d options, over the limit of %d", p.Name, len(p.Options), MaxEnumOptions)
	}
	seen := map[string]bool{}
	for _, o := range p.Options {
		if utf8.RuneCountInString(o) > MaxTextLength || !utf8.ValidString(o) || strings.ContainsRune(o, 0) {
			return invalid("enum parameter %q: an option is over %d characters or not valid text", p.Name, MaxTextLength)
		}
		if seen[o] {
			return invalid("enum parameter %q lists option %q twice", p.Name, o)
		}
		seen[o] = true
	}
	if p.OptionsQuery != "" {
		return validName("options_query", p.OptionsQuery)
	}
	return nil
}

func validateQueries(m *Manifest, declared map[string]*Param) (map[string]*Query, []error) {
	byName := map[string]*Query{}
	folded := map[string]bool{}
	var errs []error
	for i := range m.Queries {
		q := &m.Queries[i]
		if err := validName("query", q.Name); err != nil {
			errs = append(errs, err)
			continue
		}
		if folded[strings.ToLower(q.Name)] {
			errs = append(errs, invalid("query %q is declared twice (names are compared without regard to case)", q.Name))
			continue
		}
		folded[strings.ToLower(q.Name)] = true
		byName[q.Name] = q
		if err := validateQuery(q, declared); err != nil {
			errs = append(errs, err)
		}
	}
	return byName, errs
}

func validateQuery(q *Query, declared map[string]*Param) error {
	if strings.TrimSpace(q.SQL) == "" {
		return invalid("query %q has no sql", q.Name)
	}
	if len(q.SQL) > MaxSQLBytes || strings.ContainsRune(q.SQL, 0) || !utf8.ValidString(q.SQL) {
		return invalid("query %q: sql must be valid text of at most %d bytes", q.Name, MaxSQLBytes)
	}
	if q.Connection != "" {
		if err := connname.Validate(q.Connection); err != nil {
			return invalid("query %q: %v", q.Name, err)
		}
	}
	q.Uses = engine.ParamNames(q.SQL)
	for _, u := range q.Uses {
		if _, ok := declared[u]; !ok {
			return invalid("query %q uses $%s, which is not a declared parameter", q.Name, u)
		}
	}
	return nil
}

// validateOptionsQueries checks that each enum's options_query names a query,
// that the query does not use the parameter it feeds, and that no chain of
// options queries loops back (a feeds b feeds a).
func validateOptionsQueries(m *Manifest, declared map[string]*Param, queries map[string]*Query) []error {
	var errs []error
	linked := true
	for i := range m.Parameters {
		p := &m.Parameters[i]
		if p.OptionsQuery == "" {
			continue
		}
		q, ok := queries[p.OptionsQuery]
		if !ok {
			errs = append(errs, invalid("parameter %q: options_query %q is not a query of this artifact", p.Name, p.OptionsQuery))
			linked = false
			continue
		}
		for _, u := range q.Uses {
			if u == p.Name {
				errs = append(errs, invalid("parameter %q: its options_query %q may not use $%s", p.Name, q.Name, p.Name))
				linked = false
			}
		}
	}
	if !linked {
		return errs
	}
	for i := range m.Parameters {
		if err := optionsCycle(&m.Parameters[i], declared, queries, map[string]bool{}); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

func optionsCycle(p *Param, declared map[string]*Param, queries map[string]*Query, path map[string]bool) error {
	if p == nil || p.OptionsQuery == "" {
		return nil
	}
	if path[p.Name] {
		return invalid("parameter %q: its options_query depends on itself through other enum parameters", p.Name)
	}
	path[p.Name] = true
	defer delete(path, p.Name)
	q := queries[p.OptionsQuery]
	if q == nil {
		return nil
	}
	for _, u := range q.Uses {
		if err := optionsCycle(declared[u], declared, queries, path); err != nil {
			return err
		}
	}
	return nil
}

// ValidateView checks the view document the store holds against its
// manifest: within the size cap, one JSON object, and an A2UI view that
// a2ui.ValidateView accepts for the manifest's parameters and queries
// (m must have passed Validate). The error is JSON-path-prefixed, as the
// agent sees it.
func ValidateView(m *Manifest, view []byte) error {
	if m.View == ViewScript {
		return validateScript(view)
	}
	if m.View == ViewHTML {
		if len(view) > 0 {
			return invalid("view: html keeps its page under %s/, not in a view file", HTMLDir)
		}
		return nil
	}
	_, err := validatedPage(m, view)
	return err
}

// validateScript checks view.js: non-empty, within the cap, valid UTF-8 text
// without NUL bytes. It is never run here; what it may do is decided by the
// person's allow (ViewHash) and the worker it runs in.
func validateScript(src []byte) error {
	switch {
	case len(src) > MaxScriptBytes:
		return invalid("%s is %d bytes, over the %d byte limit", ScriptFile, len(src), MaxScriptBytes)
	case len(strings.TrimSpace(string(src))) == 0:
		return invalid("%s is empty", ScriptFile)
	case !utf8.Valid(src):
		return invalid("%s must be valid UTF-8 text", ScriptFile)
	}
	if r, ok := firstHiddenRune(string(src), true); ok {
		return invalid("%s contains a control or invisible character (U+%04X), which could make the code differ from what is shown", ScriptFile, r)
	}
	return nil
}

// firstHiddenRune finds a character that can make shown text differ from what
// runs or is parsed: C0 controls other than tab, newline and carriage return
// (ESC and NUL included), DEL, C1 controls, the bidi controls and the Unicode
// line and paragraph separators (U+2028, U+2029: JavaScript ends a line comment
// at them, while editors and terminals draw them inline or not at all). With
// invisible it also finds the zero-width characters, the left/right and Arabic
// marks, the soft hyphen and the interlinear annotation characters.
func firstHiddenRune(s string, invisible bool) (rune, bool) {
	for _, r := range s {
		switch {
		case r < 0x20 && r != '\t' && r != '\n' && r != '\r', r == 0x7f, r >= 0x80 && r <= 0x9f,
			r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069, r == 0x2028, r == 0x2029:
			return r, true
		case invisible && (r == 0x200e || r == 0x200f || (r >= 0x200b && r <= 0x200d) || r == 0x2060 || r == 0xfeff ||
			r == 0x00ad || r == 0x061c || (r >= 0xfff9 && r <= 0xfffc)):
			return r, true
		}
	}
	return 0, false
}

// RenderView is the view as Go validated it, re-marshalled: the one form a UI
// renders. A caller must never re-parse the stored bytes itself (Go and
// JavaScript disagree on, for one, a lone surrogate escape).
func RenderView(m *Manifest, view []byte) ([]byte, error) {
	if m.View == ViewScript || m.View == ViewHTML {
		return nil, invalid("a view: %s artifact has no A2UI page to render", m.View)
	}
	page, err := validatedPage(m, view)
	if err != nil {
		return nil, err
	}
	out, err := json.Marshal(page)
	if err != nil {
		return nil, fmt.Errorf("failed to render %s: %w", ViewFile, err)
	}
	return out, nil
}

// RenderPage validates a page that a script view (view.js) returned against the
// manifest's parameters and queries, and returns it re-marshalled: the one form
// the renderer is given. It is RenderView for a page that did not come from
// view.a2ui.json, so the same context-aware schema applies (controls, the
// `query` binding, pivot, drill) and an unknown field or type is refused.
func RenderPage(m *Manifest, page []byte) ([]byte, error) {
	if len(page) > MaxViewBytes {
		return nil, invalid("the page is %d bytes, over the %d byte limit", len(page), MaxViewBytes)
	}
	if err := checkJSONObject(page); err != nil {
		return nil, err
	}
	p, err := a2ui.ValidateView(page, viewContext(m))
	if err != nil {
		return nil, invalid("the page:\n%v", err)
	}
	out, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("failed to render the page: %w", err)
	}
	return out, nil
}

func validatedPage(m *Manifest, view []byte) (a2ui.Page, error) {
	if len(view) > MaxViewBytes {
		return a2ui.Page{}, invalid("%s is %d bytes, over the %d byte limit", ViewFile, len(view), MaxViewBytes)
	}
	if err := checkJSONObject(view); err != nil {
		return a2ui.Page{}, err
	}
	page, err := a2ui.ValidateView(view, viewContext(m))
	if err != nil {
		return a2ui.Page{}, invalid("%s:\n%v", ViewFile, err)
	}
	return page, nil
}

// viewContext is what the view validator knows of a manifest: parameter names
// and types, and query names.
func viewContext(m *Manifest) a2ui.ViewContext {
	vc := a2ui.ViewContext{}
	for _, p := range m.Parameters {
		vc.Params = append(vc.Params, a2ui.ViewParam{Name: p.Name, Type: p.Type})
	}
	for _, q := range m.Queries {
		vc.Queries = append(vc.Queries, q.Name)
	}
	return vc
}

func checkJSONObject(view []byte) error {
	trimmed := strings.TrimSpace(string(view))
	if trimmed == "" || trimmed[0] != '{' {
		return invalid("%s must be a JSON object", ViewFile)
	}
	if err := decodeSingleJSON(view); err != nil {
		return invalid("%s: %v", ViewFile, err)
	}
	return nil
}

// EscapeHidden shows s with every character firstHiddenRune would refuse
// replaced by a visible <U+XXXX> marker (carriage returns too, which can
// overwrite a line on a terminal). For printing a file that is broken on disk.
func EscapeHidden(s string) string {
	var b strings.Builder
	for _, r := range s {
		if _, hidden := firstHiddenRune(string(r), true); hidden || r == '\r' {
			fmt.Fprintf(&b, "<U+%04X>", r)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// ValidateParameters checks parameter declarations alone, the way a manifest's
// are checked (names, types, defaults, constraints), one error per problem. A
// saved query's declared parameters reuse it so both kinds of file agree on
// what a parameter is.
func ValidateParameters(ps []Param) []error {
	_, errs := validateParams(&Manifest{Parameters: ps})
	return errs
}
