package jsonenc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncode(t *testing.T) {
	b, err := Encode(map[string]string{"a": "x < y & z > w"}, "")
	require.NoError(t, err)
	assert.Equal(t, "{\"a\":\"x < y & z > w\"}\n", string(b), "no HTML escaping, trailing newline")

	b, err = Encode(map[string]int{"a": 1}, "  ")
	require.NoError(t, err)
	assert.Equal(t, "{\n  \"a\": 1\n}\n", string(b))

	_, err = Encode(make(chan int), "")
	assert.Error(t, err)
}
