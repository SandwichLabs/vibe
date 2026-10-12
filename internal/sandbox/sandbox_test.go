package sandbox

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParse(t *testing.T) {
	cases := []struct {
		in   string
		want Mode
		err  string
	}{
		{"read-only", ReadOnly, ""},
		{"read-write", ReadWrite, ""},
		{"full", Full, ""},
		{"", "", `unknown sandbox mode: "" (want read-only, read-write, or full)`},
		{"FULL", "", `unknown sandbox mode: "FULL" (want read-only, read-write, or full)`},
		{" full", "", `unknown sandbox mode: " full" (want read-only, read-write, or full)`},
		{"readonly", "", `unknown sandbox mode: "readonly" (want read-only, read-write, or full)`},
		{"root", "", `unknown sandbox mode: "root" (want read-only, read-write, or full)`},
	}
	for _, c := range cases {
		got, err := Parse(c.in)
		assert.Equal(t, c.want, got, c.in)
		if c.err == "" {
			assert.NoError(t, err, c.in)
		} else {
			assert.EqualError(t, err, c.err, c.in)
		}
	}
}

func TestRankAndTighter(t *testing.T) {
	for m, want := range map[Mode]int{ReadOnly: 0, ReadWrite: 1, Full: 2} {
		r, ok := Rank(m)
		assert.True(t, ok)
		assert.Equal(t, want, r)
	}
	_, ok := Rank("bogus")
	assert.False(t, ok)

	assert.Equal(t, ReadOnly, Tighter(ReadOnly, Full))
	assert.Equal(t, ReadOnly, Tighter(Full, ReadOnly))
	assert.Equal(t, ReadWrite, Tighter(ReadWrite, Full))
	assert.Equal(t, ReadWrite, Tighter(ReadWrite, ReadWrite))
	// An unknown value is returned as is (first-argument wins), so enforcement fails closed.
	assert.Equal(t, Mode("bogus"), Tighter("bogus", ReadOnly))
	assert.Equal(t, Mode("bogus"), Tighter(ReadOnly, "bogus"))
	assert.Equal(t, Mode(""), Tighter("", Full))
}
