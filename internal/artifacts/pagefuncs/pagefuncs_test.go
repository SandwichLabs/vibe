package pagefuncs_test

import (
	"html/template"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/assay/internal/artifacts/pagefuncs"
)

var trustedTypes = []reflect.Type{
	reflect.TypeOf(template.HTML("")), reflect.TypeOf(template.JS("")), reflect.TypeOf(template.CSS("")),
	reflect.TypeOf(template.URL("")), reflect.TypeOf(template.HTMLAttr("")), reflect.TypeOf(template.JSStr("")),
	reflect.TypeOf(template.Srcset("")),
}

// holdsTrusted reports whether t is, or contains anywhere in its type graph,
// one of html/template's trusted string types.
func holdsTrusted(t reflect.Type, seen map[reflect.Type]bool) bool {
	if seen[t] {
		return false
	}
	seen[t] = true
	for _, tt := range trustedTypes {
		if t == tt {
			return true
		}
	}
	// Methods too: a returned value's methods are callable from a template,
	// so none may return a trusted type either.
	for _, rt := range []reflect.Type{t, reflect.PointerTo(t)} {
		for i := 0; i < rt.NumMethod(); i++ {
			m := rt.Method(i).Type
			for j := 0; j < m.NumOut(); j++ {
				if holdsTrusted(m.Out(j), seen) {
					return true
				}
			}
		}
	}
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Chan:
		return holdsTrusted(t.Elem(), seen)
	case reflect.Map:
		return holdsTrusted(t.Key(), seen) || holdsTrusted(t.Elem(), seen)
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			if holdsTrusted(t.Field(i).Type, seen) {
				return true
			}
		}
	}
	return false
}

// TestNoFunctionReturnsATrustedType is the lint the plan calls its single most
// important line: a safeHTML-style helper would turn any database row into
// script. It walks every function's result types, structs and containers
// included.
func TestNoFunctionReturnsATrustedType(t *testing.T) {
	for _, funcs := range []template.FuncMap{pagefuncs.Funcs(pagefuncs.StubEnv{}), pagefuncs.FuncsMetered(pagefuncs.StubEnv{}, unlimited{})} {
		require.NotEmpty(t, funcs)
		for name, f := range funcs {
			ft := reflect.TypeOf(f)
			require.Equal(t, reflect.Func, ft.Kind(), name)
			for i := 0; i < ft.NumOut(); i++ {
				assert.Falsef(t, holdsTrusted(ft.Out(i), map[reflect.Type]bool{}), "%s returns a trusted type (%v)", name, ft.Out(i))
			}
		}
	}
}

// unlimited is a Meter that never refuses.
type unlimited struct{}

func (unlimited) Check(int64) error  { return nil }
func (unlimited) Charge(int64) error { return nil }

func TestTrustedTypeWalkerFindsThem(t *testing.T) {
	type wrapped struct{ X []map[string]template.HTML }
	assert.True(t, holdsTrusted(reflect.TypeOf(wrapped{}), map[reflect.Type]bool{}))
	assert.True(t, holdsTrusted(reflect.TypeOf(template.URL("")), map[reflect.Type]bool{}))
	assert.False(t, holdsTrusted(reflect.TypeOf(pagefuncs.Result{}), map[reflect.Type]bool{}))
}

func TestTheMapIsClosed(t *testing.T) {
	var got []string
	for name := range pagefuncs.Funcs(pagefuncs.StubEnv{}) {
		got = append(got, name)
	}
	want := []string{
		pagefuncs.Query, pagefuncs.Param, pagefuncs.Params, pagefuncs.Link, pagefuncs.Number, pagefuncs.Percent, pagefuncs.Date,
		pagefuncs.Bytes, pagefuncs.Truncate, pagefuncs.ScaleLinear, pagefuncs.ScaleBand, pagefuncs.PathLine, pagefuncs.JSON,
	}
	sort.Strings(got)
	sort.Strings(want)
	assert.Equal(t, want, got)
	assert.Equal(t, []string{
		"bytes", "date", "json", "link", "number", "param", "params", "pathLine", "percent", "query", "scaleBand", "scaleLinear", "truncate",
	}, want, "the plan's names, spelled out")
}

func TestFormatting(t *testing.T) {
	tmpl := template.Must(template.New("t").Funcs(pagefuncs.Funcs(pagefuncs.StubEnv{})).Parse(
		`{{number 1234567.891 1}}|{{number -12}}|{{percent 0.256}}|{{date "2026-01-02" "short"}}|{{bytes 1536}}|{{truncate "abcdefgh" 4}}|{{scaleLinear 0 10 0 100 5}}|{{pathLine (index . 0) (index . 1)}}`))
	var b strings.Builder
	require.NoError(t, tmpl.Execute(&b, [][]any{{0, 1}, {5, 7}}))
	assert.Equal(t, "1,234,567.9|-12|25.6%|Jan 2, 2026|1.5 KiB|abc…|50|M0.00,5.00 L1.00,7.00", b.String())
}

func TestScaleBand(t *testing.T) {
	tmpl := template.Must(template.New("t").Funcs(pagefuncs.Funcs(pagefuncs.StubEnv{})).Parse(`{{ $b := scaleBand 1 0 0 100 }}{{ $b.Start }} {{ $b.Width }}`))
	var b strings.Builder
	require.NoError(t, tmpl.Execute(&b, nil))
	assert.Equal(t, "5 90", b.String())
	bad := template.Must(template.New("t").Funcs(pagefuncs.Funcs(pagefuncs.StubEnv{})).Parse(`{{ scaleBand 2 5 0 100 }}`))
	assert.Error(t, bad.Execute(&strings.Builder{}, nil))
}

func TestBadInputsAreErrors(t *testing.T) {
	tmpl := template.Must(template.New("t").Funcs(pagefuncs.Funcs(pagefuncs.StubEnv{})).Parse(`{{number "x"}}`))
	assert.Error(t, tmpl.Execute(&strings.Builder{}, nil))
	tmpl = template.Must(template.New("t").Funcs(pagefuncs.Funcs(pagefuncs.StubEnv{})).Parse(`{{date "2026-01-02" "02/01/06"}}`))
	assert.Error(t, tmpl.Execute(&strings.Builder{}, nil), "a free Go layout is refused")
}

// json strips Go types, so a template.HTML hidden in a row is only a string.
func TestJSONIsPlainDataInAScriptBlock(t *testing.T) {
	tmpl := template.Must(template.New("t").Funcs(pagefuncs.Funcs(pagefuncs.StubEnv{})).Parse(`<script type="application/json">{{json .}}</script>`))
	var b strings.Builder
	row := map[string]any{"a": template.HTML("</script><script>alert(1)</script>"), "n": int64(1) << 60}
	require.NoError(t, tmpl.Execute(&b, row))
	out := b.String()
	assert.NotContains(t, out, "</script><script>")
	assert.Contains(t, out, "1152921504606846976")
}

type trustedMethod struct{}

func (trustedMethod) Markup() template.HTML { return "<b>" }

func TestLintCatchesATrustedTypeBehindAMethod(t *testing.T) {
	assert.True(t, holdsTrusted(reflect.TypeOf(trustedMethod{}), map[reflect.Type]bool{}))
	assert.True(t, holdsTrusted(reflect.TypeOf(&trustedMethod{}), map[reflect.Type]bool{}))
}

// hostileEnv is an Env that hands a template trusted types in every place a
// value can come from.
type hostileEnv struct{ pagefuncs.StubEnv }

type named string

func (hostileEnv) Query(string) (pagefuncs.Result, error) {
	return pagefuncs.Result{Columns: []string{"a"}, Rows: [][]any{{
		template.HTML("<b>x</b>"), template.JS("alert(1)"), template.URL("javascript:x"), named("n"),
		[]any{template.HTMLAttr(` onclick="x"`)}, map[string]any{"k": template.CSS("x{}")},
	}}}, nil
}

func (hostileEnv) Param(string) (any, error) { return template.HTML("<i>p</i>"), nil }

func TestFuncsNormalizeWhatTheEnvReturns(t *testing.T) {
	tmpl := template.Must(template.New("t").Funcs(pagefuncs.Funcs(hostileEnv{})).Parse(
		`{{ range (query "q").Rows }}{{ range . }}[{{ . }}]{{ end }}{{ end }}|{{ param "p" }}|<a href="{{ param "p" }}">`))
	var b strings.Builder
	require.NoError(t, tmpl.Execute(&b, nil))
	out := b.String()
	assert.NotContains(t, out, "<b>x</b>", "a trusted HTML value from a row is escaped")
	assert.NotContains(t, out, "<i>p</i>")
	assert.Contains(t, out, "&lt;b&gt;x&lt;/b&gt;")
	assert.Contains(t, out, `href="%3ci%3ep%3c/i%3e"`, "an attribute value is escaped like any string")
}

type plainStruct struct{ A template.HTML }

func TestFuncsNormalizeMapsAndStructsOfAnyShape(t *testing.T) {
	env := mapEnv{}
	tmpl := template.Must(template.New("t").Funcs(pagefuncs.Funcs(env)).Parse(`{{ range (query "q").Rows }}{{ range . }}[{{ . }}]{{ end }}{{ end }}`))
	var b strings.Builder
	require.NoError(t, tmpl.Execute(&b, nil))
	out := b.String()
	assert.NotContains(t, out, "<b>")
	assert.Contains(t, out, "&lt;b&gt;")
}

type mapEnv struct{ pagefuncs.StubEnv }

func (mapEnv) Query(string) (pagefuncs.Result, error) {
	return pagefuncs.Result{Columns: []string{"a"}, Rows: [][]any{{
		map[any]any{1: template.HTML("<b>1</b>"), "k": template.JS("<b>2</b>")},
		plainStruct{A: "<b>3</b>"},
	}}}, nil
}
