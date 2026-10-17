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
// means: that is the agent SQL policy's job (internal/agents).
func Validate(m *Manifest) error {
	if err := validateHeader(m); err != nil {
		return err
	}
	declared, err := validateParams(m)
	if err != nil {
		return err
	}
	queries, err := validateQueries(m, declared)
	if err != nil {
		return err
	}
	return validateOptionsQueries(m, declared, queries)
}

func validateHeader(m *Manifest) error {
	if m.Version != Version {
		return invalid("version must be %d", Version)
	}
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
	if m.View == "" {
		m.View = ViewA2UI
	}
	if m.View != ViewA2UI {
		return invalid("view must be %q", ViewA2UI)
	}
	if len(m.Queries) == 0 {
		return invalid("an artifact needs at least one query")
	}
	if len(m.Queries) > MaxQueries {
		return invalid("%d queries, over the limit of %d", len(m.Queries), MaxQueries)
	}
	if len(m.Parameters) > MaxParams {
		return invalid("%d parameters, over the limit of %d", len(m.Parameters), MaxParams)
	}
	return nil
}

func validateParams(m *Manifest) (map[string]*Param, error) {
	declared := map[string]*Param{}
	folded := map[string]bool{}
	for i := range m.Parameters {
		p := &m.Parameters[i]
		if err := validName("parameter", p.Name); err != nil {
			return nil, err
		}
		if folded[strings.ToLower(p.Name)] {
			return nil, invalid("parameter %q is declared twice (names are compared without regard to case)", p.Name)
		}
		folded[strings.ToLower(p.Name)] = true
		declared[p.Name] = p
		if err := validateParam(p); err != nil {
			return nil, err
		}
	}
	return declared, nil
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

func validateQueries(m *Manifest, declared map[string]*Param) (map[string]*Query, error) {
	byName := map[string]*Query{}
	folded := map[string]bool{}
	for i := range m.Queries {
		q := &m.Queries[i]
		if err := validName("query", q.Name); err != nil {
			return nil, err
		}
		if folded[strings.ToLower(q.Name)] {
			return nil, invalid("query %q is declared twice (names are compared without regard to case)", q.Name)
		}
		folded[strings.ToLower(q.Name)] = true
		byName[q.Name] = q
		if err := validateQuery(q, declared); err != nil {
			return nil, err
		}
	}
	return byName, nil
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
func validateOptionsQueries(m *Manifest, declared map[string]*Param, queries map[string]*Query) error {
	for i := range m.Parameters {
		p := &m.Parameters[i]
		if p.OptionsQuery == "" {
			continue
		}
		q, ok := queries[p.OptionsQuery]
		if !ok {
			return invalid("parameter %q: options_query %q is not a query of this artifact", p.Name, p.OptionsQuery)
		}
		for _, u := range q.Uses {
			if u == p.Name {
				return invalid("parameter %q: its options_query %q may not use $%s", p.Name, q.Name, p.Name)
			}
		}
	}
	for i := range m.Parameters {
		if err := optionsCycle(&m.Parameters[i], declared, queries, map[string]bool{}); err != nil {
			return err
		}
	}
	return nil
}

func optionsCycle(p *Param, declared map[string]*Param, queries map[string]*Query, path map[string]bool) error {
	if p.OptionsQuery == "" {
		return nil
	}
	if path[p.Name] {
		return invalid("parameter %q: its options_query depends on itself through other enum parameters", p.Name)
	}
	path[p.Name] = true
	defer delete(path, p.Name)
	for _, u := range queries[p.OptionsQuery].Uses {
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
	_, err := validatedPage(m, view)
	return err
}

// RenderView is the view as Go validated it, re-marshalled: the one form a UI
// renders. A caller must never re-parse the stored bytes itself (Go and
// JavaScript disagree on, for one, a lone surrogate escape).
func RenderView(m *Manifest, view []byte) ([]byte, error) {
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
