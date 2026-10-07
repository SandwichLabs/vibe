package agents

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type staticPolicy struct {
	sandbox string
	ask     bool
}

func (s staticPolicy) AgentPolicy() (string, bool) { return s.sandbox, s.ask }

func TestPolicyFromFailsClosed(t *testing.T) {
	assert.Equal(t, FilePolicy{Mode: DefaultSandboxMode, AskBeforeWrites: true}, PolicyFrom(nil))
	assert.Equal(t, FilePolicy{Mode: ModeReadOnly, AskBeforeWrites: true}, PolicyFrom(staticPolicy{"root", true}), "an unknown tier is read-only, not the default")
	assert.Equal(t, FilePolicy{Mode: ModeFull, AskBeforeWrites: false}, PolicyFrom(staticPolicy{"full", false}))
}
