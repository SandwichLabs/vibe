package agents

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

type fakeMeta map[string]string

func (f fakeMeta) Meta(k string) (string, error) {
	if v, ok := f[k]; ok {
		return v, nil
	}
	return "", errors.New("absent")
}

type staticPolicy struct {
	sandbox string
	ask     bool
}

func (s staticPolicy) AgentPolicy() (string, bool) { return s.sandbox, s.ask }

func TestMetaPolicyMatchesReadFilePolicy(t *testing.T) {
	for _, m := range []MetaReader{
		nil,
		fakeMeta{},
		fakeMeta{MetaKeySandbox: "full"},
		fakeMeta{MetaKeySandbox: "full", MetaKeyAskBeforeWrites: "false"},
		fakeMeta{MetaKeySandbox: "weird"},
		fakeMeta{MetaKeyAskBeforeWrites: "nope"},
	} {
		assert.Equal(t, ReadFilePolicy(m), PolicyFrom(MetaPolicy(m)))
	}
}

func TestPolicyFromFailsClosed(t *testing.T) {
	assert.Equal(t, FilePolicy{Mode: DefaultSandboxMode, AskBeforeWrites: true}, PolicyFrom(nil))
	assert.Equal(t, FilePolicy{Mode: ModeReadOnly, AskBeforeWrites: true}, PolicyFrom(staticPolicy{"root", true}), "an unknown tier is read-only, not the default")
	assert.Equal(t, FilePolicy{Mode: ModeFull, AskBeforeWrites: false}, PolicyFrom(staticPolicy{"full", false}))
}
