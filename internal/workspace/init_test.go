package workspace

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInit_GitIgnoresTheMachineLocalFolderPageStateLivesIn(t *testing.T) {
	assert.Contains(t, gitignoreLines, LocalDir+"/", "page state is under .slaw-local/artifacts/<name>/state.json")
}
