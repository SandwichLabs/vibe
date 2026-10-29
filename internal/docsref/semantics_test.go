package docsref

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/semantics"
)

// TestSemanticsDocNamesEveryClosedValue keeps docs/reference/semantics.md
// honest: every assertion kind, join cardinality, metric format and measured
// verdict the validator accepts, and every field of the three yaml documents,
// appears in the doc, and the doc is listed in both indexes.
func TestSemanticsDocNamesEveryClosedValue(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "reference", "semantics.md"))
	require.NoError(t, err)
	doc := string(data)

	sets := map[string][]string{
		"entity assertion kind": semantics.EntityAssertionKinds,
		"metric assertion kind": semantics.MetricAssertionKinds,
		"cardinality":           semantics.Cardinalities,
		"format":                semantics.Formats,
		"verdict":               semantics.Verdicts,
	}
	for what, values := range sets {
		require.NotEmpty(t, values, what)
		for _, v := range values {
			assert.True(t, strings.Contains(doc, "`"+v+"`"), "semantics.md does not name the %s %q", what, v)
		}
	}
	// The constants are the sets: a new constant that is not in a set fails here.
	for _, c := range []string{semantics.AssertUnique, semantics.AssertNotNull, semantics.AssertAcceptedValues, semantics.AssertNonNegative,
		semantics.OneToOne, semantics.ManyToOne, semantics.OneToMany, semantics.ManyToMany,
		semantics.FormatNumber, semantics.FormatCurrency, semantics.FormatPercent,
		semantics.VerdictConsistent, semantics.VerdictFanOut, semantics.VerdictOrphans} {
		found := false
		for _, values := range sets {
			for _, v := range values {
				found = found || v == c
			}
		}
		assert.True(t, found, "constant %q is in none of the validator's value sets", c)
	}

	for _, typ := range []reflect.Type{
		reflect.TypeOf(semantics.Entity{}), reflect.TypeOf(semantics.Column{}), reflect.TypeOf(semantics.Assertion{}),
		reflect.TypeOf(semantics.Metric{}), reflect.TypeOf(semantics.SettledBy{}),
		reflect.TypeOf(semantics.Join{}), reflect.TypeOf(semantics.JoinOn{}), reflect.TypeOf(semantics.Measured{}),
	} {
		for _, key := range yamlKeys(typ) {
			assert.True(t, strings.Contains(doc, key), "semantics.md does not mention the %s field %q", typ.Name(), key)
		}
	}

	for _, index := range []string{
		filepath.Join("docs", "reference", "README.md"),
		filepath.Join("docs", "README.md"),
	} {
		b, err := os.ReadFile(filepath.Join(repoRoot(t), index))
		require.NoError(t, err)
		assert.True(t, strings.Contains(string(b), "semantics.md"), "%s does not list semantics.md", index)
	}
}
