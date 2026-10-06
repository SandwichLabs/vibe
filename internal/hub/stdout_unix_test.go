//go:build unix

package hub

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHelperProtectStdout is the body of the subprocess run by
// TestProtectStdoutKeepsNoiseOutOfTheProtocolChannel.
func TestHelperProtectStdout(t *testing.T) {
	if os.Getenv("VIBE_HUB_TEST_STDOUT") == "" {
		t.Skip("not a helper invocation")
	}
	proto, err := ProtectStdout()
	if err != nil {
		fmt.Fprintln(os.Stderr, "protect failed:", err)
		os.Exit(2)
	}
	fmt.Println("go library noise")                    // Go-level print
	fmt.Fprintln(os.Stdout, "more go noise")           // os.Stdout is stderr now
	_, _ = syscall.Write(1, []byte("c-level noise\n")) // what a C printf ends up doing
	fmt.Fprintln(proto, `{"jsonrpc":"2.0","id":1,"result":{}}`)
	os.Exit(0)
}

func TestProtectStdoutKeepsNoiseOutOfTheProtocolChannel(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run", "^TestHelperProtectStdout$")
	cmd.Env = append(os.Environ(), "VIBE_HUB_TEST_STDOUT=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	require.NoError(t, cmd.Run(), stderr.String())

	assert.Equal(t, `{"jsonrpc":"2.0","id":1,"result":{}}`+"\n", stdout.String(), "stdout carries protocol bytes only")
	for _, noise := range []string{"go library noise", "more go noise", "c-level noise"} {
		assert.Contains(t, stderr.String(), noise, "library output is diverted to stderr, not lost")
	}
}
