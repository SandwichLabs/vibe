package workspace

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/SandwichLabs/assay/internal/semantics"
	"github.com/SandwichLabs/assay/internal/semantics/proposals"
)

// What an agent surface is handed cannot be turned back into the writer.
func TestAgentAccess_CannotBeAssertedBackToTheWriter(t *testing.T) {
	ws, _ := newWorkspace(t)
	a := NewAgentAccess(ws, nil)
	var sem any = a.Semantics()
	var props any = a.Proposals()
	_, ok := sem.(semantics.Store)
	assert.False(t, ok)
	_, ok = props.(proposals.Store)
	assert.False(t, ok)
	_, ok = sem.(interface {
		PutMetric(...any) error
	})
	assert.False(t, ok)
}
