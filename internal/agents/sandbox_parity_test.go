package agents

import (
	"testing"

	"github.com/SandwichLabs/vibe/internal/workspace"
	"github.com/stretchr/testify/assert"
)

// The workspace policy and the agent enforcement share internal/sandbox: the
// same inputs are accepted and the same error text comes back from both.
func TestSandboxParsersAgree(t *testing.T) {
	for _, in := range []string{"read-only", "read-write", "full", "", "FULL", " full", "readonly", "root", "read-only\n"} {
		m, merr := ParseSandboxMode(in)
		w, werr := workspace.ParseSandbox(in)
		assert.Equal(t, string(m), w, in)
		if merr == nil {
			assert.NoError(t, werr, in)
		} else {
			assert.EqualError(t, werr, merr.Error(), in)
		}
	}
	_, err := ParseSandboxMode("bogus")
	assert.EqualError(t, err, `unknown sandbox mode: "bogus" (want read-only, read-write, or full)`)
}
