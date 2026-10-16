package artifacts

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/SandwichLabs/vibe/internal/a2ui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const richView = `{"title":"Sales","components":[
 {"id":"c","type":"container","layout":"row"},
 {"id":"d","type":"dateRange","parent":"c","start":"from","end":"to"},
 {"id":"r","type":"select","parent":"c","param":"region"},
 {"id":"t","type":"numberInput","parent":"c","param":"top"},
 {"id":"n","type":"textInput","parent":"c","param":"note"},
 {"id":"l","type":"select","parent":"c","param":"live"},
 {"id":"b","type":"barChart","query":"by_region","drill":{"param":"region","column":"region"}},
 {"id":"p","type":"pivot","query":"by_region","rows":["region"],"columns":"month","value":"revenue"}
]}`

func TestValidateView_AgainstTheManifest(t *testing.T) {
	m := mustDecode(t, goodManifest)
	require.NoError(t, ValidateView(m, []byte(richView)))
}

func TestValidateView_Refusals(t *testing.T) {
	m := mustDecode(t, goodManifest)
	cases := []struct{ name, view, want string }{
		{"not an object", `[1]`, "must be a JSON object"},
		{"two values", `{"a":1}{"b":2}`, "more than one JSON value"},
		{"sql in a view", `{"title":"T","components":[{"id":"a","type":"table","sql":"SELECT 1"}]}`, `components[0].sql: "sql" is not allowed in a data artifact view`},
		{"missing query", `{"title":"T","components":[{"id":"a","type":"table","query":"gone"}]}`, `"gone" is not a query of this artifact`},
		{"control on the wrong type", `{"title":"T","components":[{"id":"a","type":"select","param":"note"}]}`, `parameter "note" is type text here`},
		{"dateRange over non-date", `{"title":"T","components":[{"id":"a","type":"dateRange","start":"from","end":"top"}]}`, `parameter "top" is type integer here`},
		{"drill undeclared", `{"title":"T","components":[{"id":"a","type":"table","drill":{"param":"nope","column":"c"}}]}`, `"nope" is not a declared parameter`},
		{"no components", `{"title":"T","components":[]}`, "must contain at least one component"},
		{"over the cap", `{"pad":"` + strings.Repeat("x", MaxViewBytes) + `"}`, "over the"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateView(m, []byte(tc.view))
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrInvalid)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// The store applies the same check on every path: Put, and Get of a pair
// edited outside.
func TestStore_ViewIsValidatedAgainstTheManifest(t *testing.T) {
	storeContract(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		a := newArtifact(t, "sales")
		a.View = []byte(richView)
		err := s.Put(ctx, a)
		require.NoError(t, err)

		b := newArtifact(t, "other")
		b.View = []byte(`{"title":"T","components":[{"id":"a","type":"table","query":"gone"}]}`)
		err = s.Put(ctx, b)
		require.ErrorIs(t, err, ErrInvalid)
		_, err = s.Get(ctx, "other")
		require.ErrorIs(t, err, ErrNotFound)
	})
}

func TestFileStore_EditedViewNamingAMissingQueryListsWithItsError(t *testing.T) {
	root := t.TempDir()
	s := NewFileStore(root)
	putFresh(t, s, "sales")
	bad := `{"title":"T","components":[{"id":"a","type":"table","query":"deleted_query"}]}`
	require.NoError(t, os.WriteFile(fsPath(root, "sales", ViewFile), []byte(bad), 0o644))
	_, err := s.Get(context.Background(), "sales")
	require.ErrorIs(t, err, ErrInvalid)
	list, err := s.List(context.Background())
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Contains(t, list[0].Error, "deleted_query")
}

// a2ui carries its own copy of the parameter type names (it imports nothing
// from here); they must stay the manifest's.
func TestViewParamTypesMatchTheManifest(t *testing.T) {
	assert.Equal(t, []string{TypeText, TypeNumber, TypeInteger, TypeDate, TypeBoolean, TypeEnum},
		[]string{a2ui.ParamText, a2ui.ParamNumber, a2ui.ParamInteger, a2ui.ParamDate, a2ui.ParamBoolean, a2ui.ParamEnum})
}
