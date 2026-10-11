package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConnAdd_MotherDuckIsWritableAndRefusesReadOnly(t *testing.T) {
	root := newWS(t)
	addOut, stderr, err := runVibe("conn", "add", "md", "--type", "motherduck", "--database", "my_db", "--sign-in", "env:MOTHERDUCK", "--workspace", root)
	require.NoError(t, err, stderr)
	assert.Contains(t, addOut, "MOTHERDUCK_TOKEN")
	assert.NotContains(t, addOut, "MOTHERDUCK_USER")
	assert.NotContains(t, addOut, "MOTHERDUCK_PASSWORD")
	out, stderr, err := runVibe("conn", "list", "--json", "--workspace", root)
	require.NoError(t, err, stderr)
	assert.Contains(t, out, `"read_only": false`)

	_, _, err = runVibe("conn", "add", "md2", "--type", "motherduck", "--database", "my_db", "--read-only=true", "--workspace", root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "can't be attached read-only")
}
