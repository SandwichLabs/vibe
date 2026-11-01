package pagehost_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/assay/internal/pagehost"
)

// play runs the injected API in node with a fake webview (skipped where node is
// not installed) and returns the scenario's JSON result.
func play(t *testing.T, scenario string, caps []string) map[string]any {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "api.js")
	require.NoError(t, os.WriteFile(script, []byte(pagehost.APIScript([]byte(`{}`), caps)), 0o600))
	harness, err := filepath.Abs("testdata/api_harness.js")
	require.NoError(t, err)
	cmd := exec.Command(node, harness, script, scenario)
	cmd.Dir = dir
	b, err := cmd.CombinedOutput()
	require.NoError(t, err, string(b))
	var out map[string]any
	require.NoError(t, json.Unmarshal(b, &out), string(b))
	return out
}

func TestAPI_UseResolvesNullForWhatWasNotGranted(t *testing.T) {
	out := play(t, "use", []string{"state"})
	assert.Equal(t, true, out["state"])
	assert.Equal(t, false, out["download"])
	assert.Equal(t, false, out["ask"])
	assert.Equal(t, false, out["other"], "only the closed set, not Object.prototype names")
	assert.Equal(t, false, out["proto"])
	assert.Equal(t, false, play(t, "use", nil)["state"])
}

func TestAPI_SaveIsRefusedInsideTheViewWithoutATrustedInput(t *testing.T) {
	out := play(t, "gesture", []string{"download"})
	gesture := pagehost.ErrorText[pagehost.CodeGesture]
	assert.Equal(t, gesture, out["noGesture"])
	assert.Equal(t, gesture, out["forged"], "an event the page dispatched is not trusted")
	assert.Equal(t, gesture, out["escape"])
	assert.Equal(t, gesture, out["expired"], "an input older than five seconds")
	assert.Equal(t, gesture, out["consumed"], "one input pays for one save")
	assert.Equal(t, true, out["listenersAreCapture"])
	assert.EqualValues(t, 0, out["postedWithoutGesture"], "nothing was sent to Studio")
	posted := list(t, out["posted"])
	require.Len(t, posted, 1)
	call := obj(t, posted[0])
	assert.Equal(t, "download", call["cap"])
	assert.Equal(t, "save", call["op"])
	assert.Equal(t, map[string]any{"saved": false}, out["result"])
}

func TestAPI_BinaryDataTravelsAsBase64(t *testing.T) {
	out := play(t, "bytes", []string{"download"})
	arg := obj(t, obj(t, list(t, out["posted"])[0])["arg"])
	assert.Equal(t, "base64", arg["enc"])
	assert.Equal(t, "iVBOR/8A", arg["data"])
}

func TestAPI_BigArgumentsGoAsAckedChunksUnderTheLineCap(t *testing.T) {
	out := play(t, "chunks", []string{"state"})
	assert.Less(t, num(t, out["maxBytes"]), float64(pagehost.MaxFromHelper))
	assert.Greater(t, num(t, out["puts"]), 1.0)
	assert.Equal(t, true, out["ordered"])
	assert.Equal(t, true, out["roundTrips"], "the chunks join to exactly the argument, surrogate pairs intact")
	call := obj(t, out["call"])
	assert.EqualValues(t, 1, call["up"])
	assert.Nil(t, call["arg"])
}

func TestAPI_OnChangeDeliversUntilUnsubscribed(t *testing.T) {
	out := play(t, "event", []string{"state"})
	assert.Len(t, out["got"], 1)
}

func TestAPI_ThePageCannotReplaceTheApi(t *testing.T) {
	out := play(t, "frozen", []string{"state"})
	assert.Equal(t, true, out["runIsFunction"])
	assert.Equal(t, true, out["setIsFunction"])
}

func obj(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	require.True(t, ok, "%v", v)
	return m
}

func list(t *testing.T, v any) []any {
	t.Helper()
	l, ok := v.([]any)
	require.True(t, ok, "%v", v)
	return l
}

func num(t *testing.T, v any) float64 {
	t.Helper()
	n, ok := v.(float64)
	require.True(t, ok, "%v", v)
	return n
}
