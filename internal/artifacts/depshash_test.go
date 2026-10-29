package artifacts

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDepsHash_NoPartsKeepsTheBase(t *testing.T) {
	assert.Equal(t, "abc", DepsHash("abc", nil))
}

func TestDepsHash_ChangesWithAnyPartAndIgnoresOrder(t *testing.T) {
	a := []DepPart{{"metric", "net_revenue", "r1"}, {"query", "base", "q1"}}
	h := DepsHash("base", a)
	assert.NotEqual(t, "base", h)
	assert.Equal(t, h, DepsHash("base", []DepPart{a[1], a[0]}), "input order does not matter")
	assert.NotEqual(t, h, DepsHash("base", []DepPart{{"metric", "net_revenue", "r2"}, a[1]}))
	assert.NotEqual(t, h, DepsHash("base", []DepPart{a[0], {"query", "base", "q2"}}))
	assert.NotEqual(t, h, DepsHash("base", a[:1]))
	assert.NotEqual(t, h, DepsHash("other", a))
	// A byte moved across a boundary is a different stream.
	assert.NotEqual(t, DepsHash("b", []DepPart{{"metric", "xy", "z"}}), DepsHash("b", []DepPart{{"metric", "x", "yz"}}))
}
