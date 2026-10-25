package artifacts

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateAll_ListsEveryProblem(t *testing.T) {
	m := &Manifest{
		Version: 1, Title: "",
		Parameters: []Param{{Name: "1bad", Type: "text", Default: ptr("")}, {Name: "n", Type: "wat", Default: ptr("x")}, {Name: "d", Type: "integer"}},
		Queries:    []Query{{Name: "a", SQL: "SELECT $zz"}, {Name: "b", SQL: " "}, {Name: "a", SQL: "SELECT 1"}},
	}
	errs := ValidateAll(m)
	var all []string
	for _, e := range errs {
		assert.ErrorIs(t, e, ErrInvalid)
		all = append(all, e.Error())
	}
	text := strings.Join(all, "\n")
	for _, w := range []string{"title is required", `parameter name "1bad"`, `type "wat"`, `parameter "d" needs a default`, "$zz", `query "b" has no sql`, `query "a" is declared twice`} {
		assert.Contains(t, text, w)
	}
	// Validate is the first of them.
	assert.EqualError(t, Validate(m), errs[0].Error())
}

func TestValidateAll_GoodManifestHasNone(t *testing.T) {
	assert.Empty(t, ValidateAll(mustDecode(t, goodManifest)))
}

func ptr(s string) *string { return &s }

func TestParamYAML_DefaultsKeepTheirType(t *testing.T) {
	m := mustDecode(t, goodManifest)
	m.Parameters = append(m.Parameters,
		Param{Name: "big", Type: TypeNumber, Default: ptr("1000000")},
		Param{Name: "tiny", Type: TypeNumber, Default: ptr("0.00001")},
		Param{Name: "odd", Type: TypeNumber, Default: ptr("1.50")},
		Param{Name: "flag", Type: TypeBoolean, Default: ptr("true")},
		Param{Name: "word", Type: TypeText, Default: ptr("true")},
		Param{Name: "zero", Type: TypeText, Default: ptr("0")},
		Param{Name: "lead", Type: TypeInteger, Default: ptr("007")},
	)
	out, err := Encode(m)
	require.NoError(t, err)
	text := string(out)
	assert.Contains(t, text, "default: 10\n", "an integer default is a YAML integer")
	assert.Contains(t, text, "default: true\n")
	assert.Contains(t, text, `default: "true"`, "a text default that reads as a boolean stays text")
	assert.Contains(t, text, `default: "0"`)
	assert.Contains(t, text, `default: "1.50"`, "a non-canonical number stays text so it reads back unchanged")

	back, err := Decode(out)
	require.NoError(t, err)
	for i, p := range m.Parameters {
		require.NotNil(t, back.Parameters[i].Default)
		assert.Equal(t, *p.Default, *back.Parameters[i].Default, p.Name)
	}
	// Files written the old way (every default a string) still load.
	old := strings.Replace(goodManifest, `default: "10"`, `default: 10`, 1)
	_, err = Decode([]byte(old))
	assert.NoError(t, err)
}
