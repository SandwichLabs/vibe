package secrets

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWithFresh(t *testing.T) {
	ctx := context.Background()
	assert.False(t, IsFresh(ctx))
	assert.True(t, IsFresh(WithFresh(ctx)))
}
