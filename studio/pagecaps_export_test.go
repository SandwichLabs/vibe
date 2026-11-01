package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/assay/internal/pagestate"
)

// stateStoreForTest is a second store over the same file, as another window
// (or another Studio) would have.
func (h *pageHost) stateStoreForTest(t *testing.T, root, name string) *pagestate.Store {
	t.Helper()
	s, err := pagestate.Open(root, name)
	require.NoError(t, err)
	return s
}
