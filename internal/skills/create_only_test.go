package skills

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileStorePutWithEmptyRevIsCreateOnly(t *testing.T) {
	ctx := context.Background()
	s := NewFileStore(t.TempDir())
	require.NoError(t, s.Put(ctx, &Stored{Name: "a-skill", Description: "d", Instructions: "one"}, WithRev("")))

	// A List refreshes last-seen, which must not stand in for a missing rev.
	_, err := s.List(ctx)
	require.NoError(t, err)
	err = s.Put(ctx, &Stored{Name: "a-skill", Description: "d", Instructions: "two"}, WithRev(""))
	require.ErrorIs(t, err, ErrConflict)

	got, err := s.Get(ctx, "a-skill")
	require.NoError(t, err)
	assert.Equal(t, "one", got.Instructions)
	require.NoError(t, s.Put(ctx, &Stored{Name: "a-skill", Description: "d", Instructions: "two"}, WithRev(got.Rev)))
}
