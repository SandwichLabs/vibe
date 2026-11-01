package strictyaml

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type doc struct {
	Name string   `yaml:"name"`
	Tags []string `yaml:"tags"`
}

func decode(s string) (doc, error) {
	var d doc
	err := Decode([]byte(s), 256, 20, "x.yaml", &d)
	return d, err
}

func TestDecode_Good(t *testing.T) {
	d, err := decode("name: a\ntags: [b, c]\n")
	require.NoError(t, err)
	assert.Equal(t, doc{Name: "a", Tags: []string{"b", "c"}}, d)
}

func TestDecode_Refusals(t *testing.T) {
	cases := map[string]struct{ in, msg string }{
		"oversize":       {"name: " + strings.Repeat("a", 300), "over the 256 byte limit"},
		"control":        {"name: a\x07b\n", "control character (U+0007)"},
		"bidi":           {"name: a‮b\n", "control character (U+202E)"},
		"anchor":         {"name: &a x\n", "anchors or aliases"},
		"alias":          {"name: &a x\ntags: [*a]\n", "anchors or aliases"},
		"tag":            {"name: !!str x\n", "explicit YAML tags"},
		"merge":          {"name: a\n<<: {tags: [x]}\n", "merge keys"},
		"unknown key":    {"name: a\nother: 1\n", "other"},
		"duplicate":      {"name: a\nname: b\n", "already defined"},
		"empty":          {"", "is empty"},
		"two docs":       {"name: a\n---\nname: b\n", "single YAML document"},
		"not yaml":       {"name: [a\n", "x.yaml"},
		"too many nodes": {"tags: [" + strings.Repeat("a,", 30) + "a]\n", "too many YAML nodes"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := decode(c.in)
			require.Error(t, err)
			var se *Error
			require.True(t, errors.As(err, &se), "every refusal is an *Error")
			assert.Contains(t, err.Error(), c.msg)
			assert.NotContains(t, err.Error(), "invalid", "no package prefix: callers add their own sentinel")
		})
	}
}

func TestFirstHiddenRune(t *testing.T) {
	_, ok := FirstHiddenRune("plain\ttext\nwith\r\nbreaks", true)
	assert.False(t, ok)
	_, ok = FirstHiddenRune("a​b", false)
	assert.False(t, ok, "zero-width is only found with invisible")
	r, ok := FirstHiddenRune("a​b", true)
	assert.True(t, ok)
	assert.Equal(t, rune(0x200b), r)
}

func TestDecode_ErrorsNeverEchoRawControlCharacters(t *testing.T) {
	// The parser quotes the key it did not know; the escape (a real ESC, the
	// bidi override) must not reach a terminal.
	_, err := decode("name: a\n\"k\\u001b[31m\\u202e\": 1\n")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "\x1b")
	assert.NotContains(t, err.Error(), "\u202e")
	assert.Contains(t, err.Error(), "<U+001B>", "the unknown key is echoed, escaped")
	_, err = decode("name: a\x1b[2J\n")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "\x1b")
	assert.Equal(t, "a<U+001B>b<U+000D>c\nd", EscapeHidden("a\x1bb\rc\nd"))
}

func TestDecode_FlowMappingHint(t *testing.T) {
	type assertion struct {
		Kind   string   `yaml:"kind"`
		Column string   `yaml:"column"`
		Values []string `yaml:"values"`
	}
	type file struct {
		Assertions []assertion `yaml:"assertions"`
	}
	const hint = "inside {...} quote a value that contains , ( ) : or #"
	decodeFile := func(in string) error {
		var f file
		return Decode([]byte(in), 512, 40, "m.yaml", &f)
	}

	// The unquoted (key, unit) splits at the comma: a type error, then an unknown field "unit)".
	err := decodeFile("assertions: [{kind: accepted_values, column: unit, values: (key, unit)}]\n")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "line 1: field unit) not found")
	assert.Contains(t, err.Error(), hint)

	// A flow mapping that does not parse at all.
	err = decodeFile("assertions: [{kind: x, values: k: u}]\n")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not find expected ',' or '}'")
	assert.Contains(t, err.Error(), hint)

	// An ordinary unknown key, a block-style document and a flow-sequence error carry no hint.
	for _, in := range []string{
		"assertions: [{kind: x, zz: 1}]\n",
		"zz: 1\n",
		"assertions: [1, 2\nb: 3\n",
	} {
		err = decodeFile(in)
		require.Error(t, err, in)
		assert.NotContains(t, err.Error(), hint, in)
	}
}
