package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/prefs"
	"github.com/SandwichLabs/vibe/internal/selfupdate"
)

// updateRig is a fake GitHub plus a temp machine; tag is the "latest" release.
type updateRig struct {
	srv      *httptest.Server
	cfg      string
	data     string
	exe      string
	manifest string
	script   []byte
}

func newUpdateRig(t *testing.T, current string, managed bool) *updateRig {
	t.Helper()
	root := t.TempDir()
	r := &updateRig{cfg: filepath.Join(root, "cfg"), data: filepath.Join(root, "data")}
	bin := filepath.Join(root, "bin")
	require.NoError(t, os.MkdirAll(filepath.Join(r.data, "vibe"), 0o755))
	require.NoError(t, os.MkdirAll(bin, 0o755))
	r.exe = filepath.Join(bin, "vibe")
	require.NoError(t, os.WriteFile(r.exe, []byte("x"), 0o755))
	r.manifest = filepath.Join(r.data, "vibe", "install-manifest")
	if managed {
		require.NoError(t, os.WriteFile(r.manifest, []byte("version=0.1.1\nF "+strings.Repeat("a", 64)+" "+r.exe+"\n"), 0o644))
	}
	r.script = []byte("#!/bin/sh\necho \"installing $2\"\nsed -i.bak 's/^version=.*/version=0.1.2/' \"" + r.manifest + "\"\n")
	sum := sha256.Sum256(r.script)
	r.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/releases/latest"):
			http.Redirect(w, req, "/SandwichLabs/vibe/releases/tag/v0.1.2", http.StatusFound)
		case strings.Contains(req.URL.Path, "/releases/tags/"):
			_, _ = w.Write([]byte(`{"body":"Fixes things.","html_url":""}`))
		case strings.HasSuffix(req.URL.Path, "/checksums.txt"):
			_, _ = w.Write([]byte(hex.EncodeToString(sum[:]) + "  install.sh\n"))
		case strings.HasSuffix(req.URL.Path, "/install.sh"):
			_, _ = w.Write(r.script)
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(r.srv.Close)

	old := newUpdateService
	t.Cleanup(func() { newUpdateService = old })
	newUpdateService = func() (*selfupdate.Service, error) {
		u := selfupdate.New(current,
			selfupdate.WithHTTPClient(r.srv.Client()), selfupdate.WithBaseURLs(r.srv.URL, r.srv.URL+"/api"),
			selfupdate.WithGetenv(func(k string) string {
				if k == "XDG_DATA_HOME" {
					return r.data
				}
				return ""
			}),
			selfupdate.WithPlatform("linux"),
			selfupdate.WithExecutable(func() (string, error) { return r.exe, nil }))
		return selfupdate.NewService(u, prefs.NewRecents(prefs.NewStore(r.cfg), nil), nil), nil
	}
	return r
}

// runUpdateCmd runs `vibe update <args>` with stdin that is not a terminal.
func runUpdateCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	updateCheckOnly, updateYes = false, false
	t.Cleanup(func() { updateCheckOnly, updateYes = false, false })
	c := &cobra.Command{Use: "update", RunE: runUpdate, Args: cobra.NoArgs}
	c.Flags().BoolVar(&updateCheckOnly, "check", false, "")
	c.Flags().BoolVar(&updateYes, "yes", false, "")
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&out)
	c.SetIn(strings.NewReader("y\n")) // never a terminal: a piped "y" must not install
	c.SetArgs(args)
	err := c.ExecuteContext(t.Context())
	return out.String(), err
}

func TestUpdate_NoTerminalMeansCheckOnly(t *testing.T) {
	r := newUpdateRig(t, "v0.1.1", true)
	out, err := runUpdateCmd(t)
	require.NoError(t, err)
	assert.Contains(t, out, "Current: v0.1.1")
	assert.Contains(t, out, "Latest:  v0.1.2")
	assert.Contains(t, out, "A newer release is available")
	assert.Contains(t, out, "Fixes things.")
	assert.Contains(t, out, "vibe update --yes")
	b, _ := os.ReadFile(r.manifest)
	assert.Contains(t, string(b), "version=0.1.1", "nothing was installed")
}

func TestUpdate_CheckFlagNeverInstalls(t *testing.T) {
	r := newUpdateRig(t, "v0.1.1", true)
	out, err := runUpdateCmd(t, "--check", "--yes")
	require.NoError(t, err)
	assert.NotContains(t, out, "Installing")
	b, _ := os.ReadFile(r.manifest)
	assert.Contains(t, string(b), "version=0.1.1")
}

func TestUpdate_YesInstalls(t *testing.T) {
	r := newUpdateRig(t, "v0.1.1", true)
	out, err := runUpdateCmd(t, "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, "installing v0.1.2")
	assert.Contains(t, out, "Updated 0.1.1 -> 0.1.2")
	assert.Contains(t, out, "keeps the old version until you restart")
	b, _ := os.ReadFile(r.manifest)
	assert.Contains(t, string(b), "version=0.1.2")
}

func TestUpdate_UpToDateAndDev(t *testing.T) {
	newUpdateRig(t, "v0.1.2", true)
	out, err := runUpdateCmd(t, "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, "up to date")

	newUpdateRig(t, "dev", false)
	out, err = runUpdateCmd(t, "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, "development build")
	assert.NotContains(t, out, "newer release")
}

func TestUpdate_ExternalInstallIsNotifyOnly(t *testing.T) {
	newUpdateRig(t, "v0.1.1", false)
	out, err := runUpdateCmd(t, "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, "newer release is available")
	assert.Contains(t, out, "update it the way you installed it")
	assert.NotContains(t, out, "Installing")
}

func TestUpdate_ReportsAFailedCheck(t *testing.T) {
	r := newUpdateRig(t, "v0.1.1", true)
	r.srv.Close()
	_, err := runUpdateCmd(t)
	require.Error(t, err)
}

func TestConfigUpdates(t *testing.T) {
	r := newUpdateRig(t, "v0.1.1", true)
	run := func(args ...string) string {
		c := &cobra.Command{Use: "updates", RunE: configUpdatesCmd.RunE, Args: cobra.MaximumNArgs(1)}
		var out bytes.Buffer
		c.SetOut(&out)
		c.SetArgs(args)
		require.NoError(t, c.ExecuteContext(t.Context()))
		return out.String()
	}
	assert.Contains(t, run(), "not set")
	assert.Contains(t, run("weekly"), "weekly")
	assert.Contains(t, run(), "Update checks: weekly")
	assert.Equal(t, "weekly", prefs.NewRecents(prefs.NewStore(r.cfg), nil).UpdateSettings().Check)
	assert.Contains(t, run("off"), "off")

	c := &cobra.Command{Use: "updates", RunE: configUpdatesCmd.RunE, Args: cobra.MaximumNArgs(1)}
	c.SetOut(&bytes.Buffer{})
	c.SetErr(&bytes.Buffer{})
	c.SetArgs([]string{"daily"})
	assert.Error(t, c.ExecuteContext(t.Context()))
}
