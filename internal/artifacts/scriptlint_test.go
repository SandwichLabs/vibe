package artifacts

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCheckScriptSyntax_AcceptsRealScripts(t *testing.T) {
	for name, src := range map[string]string{
		"starter":                "function draw(ctx) { return { svg: '<svg xmlns=\"http://www.w3.org/2000/svg\"/>' } }\n",
		"async arrow":            "const draw = async ({ params, width }) => {\n  const r = await vibe.run('q', { from: params.from })\n  return { svg: `<svg width=\"${width}\">${r.rows.map((x, i) => `<rect x=\"${i * 10}\" y=\"${x[1] / 2}\"/>`).join('')}</svg>` }\n}\nself.draw = draw",
		"regex and division":     "function draw(c) { const a = c.width / 2 / 3; const re = /[/\"']+\\/x/gi; return re.test('a') ? a / 1 : 0 }",
		"regex after return":     "function draw(c) {\n  return /\\d+/.test('1')\n}",
		"comments with brackets": "// ( [ {\n/* ) ] } ' \" ` */\nfunction draw(c) { return 1 } // done (",
		"strings with brackets":  "function draw(c) { return '(' + \"[\" + `{` }",
		"template with nested template and braces": "function draw(c) { return `a ${ `b ${ {x: 1}.x }` } c` }",
		"escaped quote":            `function draw(c) { return 'it\'s' + "say \"hi\"" }`,
		"object and arrays":        "function draw(c) { const o = { a: [1, 2, { b: (3) }], c: () => ({}) }; return o }",
		"optional chaining":        "function draw(c) { return c?.params?.x ?? [] }",
		"class":                    "class A { #p = 1; static s = 2; get p() { return this.#p } }\nfunction draw() { return new A().p }",
		"regex class with slash":   "function draw() { return 'a/b'.split(/[/]/) }",
		"postfix then division":    "function draw(c) { let i = 1; return i++ / 2 + i-- / 3 }",
		"regex after if condition": "function draw(s) { if (s) /[(]/.test(s); return 1 }",
		"crlf continuation":        "function draw() { return 'a\\\r\nb' }",
		"draw via bracket":         "self['draw'] = function () { return 1 }",
		"division after paren":     "function draw(c) { return (c.width) / 2 }",
		"line continuation":        "function draw() { return 'a\\\nb' }",
	} {
		t.Run(name, func(t *testing.T) { assert.Empty(t, CheckScriptSyntax(src), src) })
	}
}

func TestCheckScriptSyntax_FindsWhatBreaksALoad(t *testing.T) {
	for name, tc := range map[string]struct{ src, want string }{
		"unclosed paren":        {"function draw( {", `"{" is never closed`},
		"unclosed brace":        {"function draw(c) {\n  return 1\n", "line 1"},
		"extra close":           {"function draw(c) { return 1 } }", `"}" has no matching "{"`},
		"mismatch":              {"function draw(c) {\n return [1, 2)\n}", `")" closes the "[" opened on line 2`},
		"unterminated string":   {"function draw(c) {\n return 'abc\n}", "line 2: a string starting with ' is not closed"},
		"unterminated template": {"function draw(c) { return `abc }", "template literal"},
		"unterminated hole":     {"function draw(c) { return `a ${ 1 ` }", "never closed"},
		"unterminated comment":  {"function draw(c) { /* x", "/* comment is never closed"},
		"unterminated regex":    {"function draw(c) { return /abc\n}", "regular expression"},
		"no draw":               {"var x = 1", "defines no draw"},
	} {
		t.Run(name, func(t *testing.T) {
			got := CheckScriptSyntax(tc.src)
			if assert.NotEmpty(t, got, tc.src) {
				assert.Contains(t, got[0], tc.want)
			}
		})
	}
}
