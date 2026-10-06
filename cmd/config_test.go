package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadSecretLineFromPipe(t *testing.T) {
	var errW bytes.Buffer
	got, err := readSecretLine(strings.NewReader("  sk-test-key \nrest\n"), &errW, "key: ")
	require.NoError(t, err)
	assert.Equal(t, "sk-test-key", got)
	assert.Empty(t, errW.String(), "no prompt when not on a terminal")

	got, err = readSecretLine(strings.NewReader("no-newline"), &errW, "key: ")
	require.NoError(t, err)
	assert.Equal(t, "no-newline", got)

	_, err = readSecretLine(strings.NewReader(""), &errW, "key: ")
	assert.Error(t, err)
}
