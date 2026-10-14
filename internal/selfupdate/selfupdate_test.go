package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/prefs"
)

// fakeGH stands in for github.com and api.github.com over TLS. Handlers read the
// mutable fields under mu, so a test sets them before the call it makes.
type fakeGH struct {
	srv *httptest.Server
	mu  sync.Mutex

	location  string            // Location of releases/latest ("" = derive from latest)
	latest    string            // tag the redirect names
	status    int               // status of releases/latest (0 = 302)
	notesBody string            // raw JSON of the release API ("" = 404)
	files     map[string][]byte // download assets by name
	redirect  map[string]string // asset name -> redirect target
	hits      []string
}

func newFakeGH(t *testing.T) *fakeGH {
	t.Helper()
	f := &fakeGH{latest: "v0.1.2", files: map[string][]byte{}, redirect: map[string]string{}}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGH) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hits = append(f.hits, r.URL.Path+" UA="+r.UserAgent()+" cookie="+r.Header.Get("Cookie")+" auth="+r.Header.Get("Authorization"))
	switch {
	case r.URL.Path == "/SandwichLabs/vibe/releases/latest":
		if f.status != 0 {
			w.WriteHeader(f.status)
			return
		}
		loc := f.location
		if loc == "" {
			loc = f.srv.URL + "/SandwichLabs/vibe/releases/tag/" + f.latest
		}
		if loc != "-" {
			w.Header().Set("Location", loc)
		}
		w.WriteHeader(http.StatusFound)
	case strings.HasPrefix(r.URL.Path, "/api/repos/SandwichLabs/vibe/releases/tags/"):
		if f.notesBody == "" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(f.notesBody))
	case strings.Contains(r.URL.Path, "/releases/download/"):
		name := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		if to, ok := f.redirect[name]; ok {
			http.Redirect(w, r, to, http.StatusFound)
			return
		}
		if b, ok := f.files[name]; ok {
			_, _ = w.Write(b)
			return
		}
		http.NotFound(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeGH) updater(version string, opts ...Option) *Updater {
	base := []Option{WithHTTPClient(f.srv.Client()), WithBaseURLs(f.srv.URL, f.srv.URL+"/api")}
	return New(version, append(base, opts...)...)
}

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// ---------------------------------------------------------------- check

func TestCheck_ParsesTheTagFromTheRedirect(t *testing.T) {
	f := newFakeGH(t)
	u := f.updater("v0.1.1")
	res, err := u.Check(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, "v0.1.2", res.Latest)
	assert.True(t, res.UpdateAvailable)
	assert.Equal(t, f.srv.URL+"/SandwichLabs/vibe/releases/tag/v0.1.2", res.ReleaseURL)
	for _, h := range f.hits {
		assert.Contains(t, h, "UA=vibe-update", "the fixed User-Agent, with no version")
		assert.Contains(t, h, "cookie= auth=", "no cookies, no auth")
	}
}

func TestCheck_RedirectProblems(t *testing.T) {
	cases := map[string]struct {
		loc    string
		status int
	}{
		"missing Location":   {loc: "-"},
		"non-github host":    {loc: "https://evil.example/SandwichLabs/vibe/releases/tag/v9.9.9"},
		"junk tag":           {loc: "SELF/SandwichLabs/vibe/releases/tag/v1.2.3;rm -rf"},
		"not a tag":          {loc: "SELF/SandwichLabs/vibe/releases"},
		"other repository":   {loc: "SELF/Other/repo/releases/tag/v1.2.3"},
		"no redirect at all": {status: http.StatusOK},
		"server error":       {status: http.StatusInternalServerError},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFakeGH(t)
			f.status = c.status
			f.location = strings.Replace(c.loc, "SELF", f.srv.URL, 1)
			_, err := f.updater("v0.1.1").Check(context.Background(), "")
			require.Error(t, err)
		})
	}
}

func TestCheck_AcceptsARelativeLocation(t *testing.T) {
	f := newFakeGH(t)
	f.location = "/SandwichLabs/vibe/releases/tag/v0.3.0"
	res, err := f.updater("v0.1.1").Check(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, "v0.3.0", res.Latest)
}

func TestCompare(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"v0.1.1", "v0.1.2", true},
		{"0.1.1", "v0.1.2", true}, // ldflags may stamp without the v
		{"v0.1.2", "v0.1.2", false},
		{"v0.2.0", "v0.1.9", false},
		{"v0.2.0-rc1", "v0.2.0", true}, // a prerelease is older than its release
		{"v0.2.0", "v0.2.1-rc1", true},
		{"dev", "v9.9.9", false},
		{"", "v9.9.9", false},
		{"not-a-version", "v9.9.9", false},
	}
	for _, c := range cases {
		f := newFakeGH(t)
		f.latest = c.latest
		res, err := f.updater(c.current).Check(context.Background(), "")
		require.NoError(t, err)
		assert.Equal(t, c.want, res.UpdateAvailable, "%s -> %s", c.current, c.latest)
	}
}

func TestCheck_SkippedVersion(t *testing.T) {
	f := newFakeGH(t)
	res, err := f.updater("v0.1.1").Check(context.Background(), "v0.1.2")
	require.NoError(t, err)
	assert.True(t, res.UpdateAvailable, "still available: Check now offers it")
	assert.True(t, res.Skipped)
	res, err = f.updater("v0.1.1").Check(context.Background(), "v0.1.0")
	require.NoError(t, err)
	assert.False(t, res.Skipped, "skipping an older version does not hide a newer one")
}

func TestCheck_NotesAreBestEffortPlainTextAndOnlyWhenNewer(t *testing.T) {
	f := newFakeGH(t)
	f.notesBody = `{"body":"## Fixes\r\n- a\u001b[31mred\u001b[0m\u0007","html_url":"` + f.srv.URL + `/SandwichLabs/vibe/releases/tag/v0.1.2"}`
	res, err := f.updater("v0.1.1").Check(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, "## Fixes\n- a[31mred[0m", res.Notes, "control characters are stripped, markdown stays as text")

	f.hits = nil
	_, err = f.updater("v0.1.2").Check(context.Background(), "")
	require.NoError(t, err)
	assert.Len(t, f.hits, 1, "up to date: one request, no notes lookup")

	f.notesBody = ""
	res, err = f.updater("v0.1.1").Check(context.Background(), "")
	require.NoError(t, err, "no notes is not an error")
	assert.Empty(t, res.Notes)
	assert.Equal(t, f.srv.URL+"/SandwichLabs/vibe/releases/tag/v0.1.2", res.ReleaseURL)
}

func TestCheck_NotesLimits(t *testing.T) {
	f := newFakeGH(t)
	f.notesBody = `{"body":"` + strings.Repeat("a", maxNotesBytes+10) + `"}`
	res, err := f.updater("v0.1.1").Check(context.Background(), "")
	require.NoError(t, err)
	assert.Empty(t, res.Notes, "over the 256 KiB cap: dropped, link only")

	f.notesBody = `{"body":"` + strings.Repeat("b", maxNotesRunes+500) + `"}`
	res, err = f.updater("v0.1.1").Check(context.Background(), "")
	require.NoError(t, err)
	assert.LessOrEqual(t, len([]rune(res.Notes)), maxNotesRunes+2)
	assert.True(t, strings.HasSuffix(res.Notes, "…"))
}

func TestCheck_ForeignReleaseURLIsIgnored(t *testing.T) {
	f := newFakeGH(t)
	f.notesBody = `{"body":"x","html_url":"https://evil.example/phish"}`
	res, err := f.updater("v0.1.1").Check(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, f.srv.URL+"/SandwichLabs/vibe/releases/tag/v0.1.2", res.ReleaseURL)
}

func TestHTTPHygiene(t *testing.T) {
	u := New("v0.1.0")
	_, err := u.checkURL("http://github.com/x")
	assert.Error(t, err, "http is refused")
	_, err = u.checkURL("https://user:pw@github.com/x")
	assert.Error(t, err, "credentials in a URL are refused")
	_, err = u.checkURL("https://github.com/x")
	assert.NoError(t, err)
	for host, want := range map[string]bool{
		"github.com": true, "github.com:443": true, "GitHub.com": true,
		"objects.githubusercontent.com": true, "release-assets.githubusercontent.com": true,
		"api.github.com": false, "github.com:8443": false, "github.com.evil.example": false,
		"evilgithub.com": false, "evil.example": false, "user@github.com": false, "": false,
	} {
		assert.Equal(t, want, u.hostAllowed(host), host)
	}
	assert.True(t, u.hostAllowed("api.github.com", "api.github.com"), "the notes fetch may reach the API host")
	assert.False(t, u.hostAllowed("evil.example", "api.github.com"))
}

func TestRedirectAllowList(t *testing.T) {
	f := newFakeGH(t)
	evil := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("pwned")) }))
	t.Cleanup(evil.Close)
	f.redirect["checksums.txt"] = evil.URL + "/checksums.txt"
	u := f.updater("v0.1.1")
	_, err := u.getOK(context.Background(), f.srv.URL+"/SandwichLabs/vibe/releases/download/v0.1.2/checksums.txt", 1024, time.Second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing redirect")

	f.redirect["checksums.txt"] = f.srv.URL + "/SandwichLabs/vibe/releases/download/v0.1.2/other"
	f.files["other"] = []byte("fine")
	b, err := u.getOK(context.Background(), f.srv.URL+"/SandwichLabs/vibe/releases/download/v0.1.2/checksums.txt", 1024, time.Second)
	require.NoError(t, err)
	assert.Equal(t, "fine", string(b))
}

func TestSizeCap(t *testing.T) {
	f := newFakeGH(t)
	f.files["big"] = []byte(strings.Repeat("x", 100))
	_, err := f.updater("v0.1.1").getOK(context.Background(), f.srv.URL+"/SandwichLabs/vibe/releases/download/v/big", 99, time.Second)
	assert.ErrorIs(t, err, ErrTooLarge)
	b, err := f.updater("v0.1.1").getOK(context.Background(), f.srv.URL+"/SandwichLabs/vibe/releases/download/v/big", 100, time.Second)
	require.NoError(t, err)
	assert.Len(t, b, 100)
}

func TestDevHooks(t *testing.T) {
	env := map[string]string{"VIBE_UPDATE_BASE_URL": "http://127.0.0.1:9/", "VIBE_UPDATE_PRETEND_VERSION": "v0.1.0"}
	getenv := func(k string) string { return env[k] }

	dev := New("dev", WithGetenv(getenv))
	assert.Equal(t, "http://127.0.0.1:9", dev.webBase)
	assert.Equal(t, "http://127.0.0.1:9/api", dev.apiBase)
	assert.Equal(t, "v0.1.0", dev.current)
	assert.True(t, dev.insecure)
	assert.Equal(t, "dev", dev.Version())

	for _, v := range []string{"v0.1.1", "0.1.1", ""} {
		rel := New(v, WithGetenv(getenv))
		assert.Equal(t, defaultWebBase, rel.webBase, "a release build ignores the base-URL hook (%q)", v)
		assert.Equal(t, defaultAPIBase, rel.apiBase)
		assert.Equal(t, v, rel.current, "and the pretend-version hook")
		assert.False(t, rel.insecure)
		assert.Empty(t, rel.dlBase)
	}
}

// ---------------------------------------------------------------- manifest + detection

// layout is a fake machine: an install dir with vibe (and vibe-studio), a manifest in a temp XDG_DATA_HOME.
type layout struct {
	home, data, bin, manifest string
	vibe                      string
}

func newLayout(t *testing.T, studio bool, version string) layout {
	t.Helper()
	root := t.TempDir()
	l := layout{home: filepath.Join(root, "home"), data: filepath.Join(root, "data"), bin: filepath.Join(root, "bin")}
	require.NoError(t, os.MkdirAll(filepath.Join(l.data, "vibe"), 0o755))
	require.NoError(t, os.MkdirAll(l.bin, 0o755))
	l.vibe = filepath.Join(l.bin, "vibe")
	require.NoError(t, os.WriteFile(l.vibe, []byte("vibe"), 0o755))
	m := "version=" + version + "\nF " + strings.Repeat("a", 64) + " " + l.vibe + "\n"
	if studio {
		st := filepath.Join(l.bin, "vibe-studio")
		require.NoError(t, os.WriteFile(st, []byte("studio"), 0o755))
		m += "F " + strings.Repeat("b", 64) + " " + st + "\n"
	}
	l.manifest = filepath.Join(l.data, "vibe", "install-manifest")
	require.NoError(t, os.WriteFile(l.manifest, []byte(m), 0o644))
	return l
}

func (l layout) opts(exe string) []Option {
	return []Option{
		WithGetenv(func(k string) string {
			if k == "XDG_DATA_HOME" {
				return l.data
			}
			return ""
		}),
		WithHomeDir(func() (string, error) { return l.home, nil }),
		WithExecutable(func() (string, error) { return exe, nil }),
		WithPlatform("linux"),
	}
}

func TestManifestPathMatchesInstallSh(t *testing.T) {
	home := func() (string, error) { return "/h", nil }
	none := func(string) string { return "" }
	p, _ := New("v1.0.0", WithPlatform("linux"), WithHomeDir(home), WithGetenv(none)).manifestPath()
	assert.Equal(t, "/h/.local/share/vibe/install-manifest", p)
	p, _ = New("v1.0.0", WithPlatform("linux"), WithHomeDir(home), WithGetenv(func(string) string { return "/x" })).manifestPath()
	assert.Equal(t, "/x/vibe/install-manifest", p)
	p, _ = New("v1.0.0", WithPlatform("darwin"), WithHomeDir(home), WithGetenv(func(string) string { return "/x" })).manifestPath()
	assert.Equal(t, "/h/Library/Application Support/vibe/install-manifest", p, "macOS ignores XDG_DATA_HOME, as install.sh does")
}

func TestReadManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "m")
	body := "version=0.1.1\nF " + strings.Repeat("a", 64) + " /opt/my bin/vibe\nF " + strings.Repeat("b", 64) + " /opt/my bin/vibe-studio\n" +
		"D - /Users/me/Applications/Vibe Studio.app\nF " + strings.Repeat("c", 64) + " relative/ignored\nnoise\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	in, err := ReadManifest(path)
	require.NoError(t, err)
	assert.Equal(t, "0.1.1", in.Version)
	assert.Equal(t, "/opt/my bin", in.InstallDir, "paths with spaces survive")
	assert.True(t, in.Studio)
	assert.Equal(t, "/Users/me/Applications", in.AppDir)
	assert.Equal(t, "/Users/me/Applications/Vibe Studio.app", in.StudioPath)

	cli := filepath.Join(dir, "c")
	require.NoError(t, os.WriteFile(cli, []byte("version=0.1.1\nF "+strings.Repeat("a", 64)+" /opt/bin/vibe\n"), 0o644))
	in, err = ReadManifest(cli)
	require.NoError(t, err)
	assert.False(t, in.Studio, "no studio entry: --cli-only")
}

func TestDetect(t *testing.T) {
	l := newLayout(t, true, "0.1.1")
	in, err := New("v0.1.1", l.opts(l.vibe)...).Detect()
	require.NoError(t, err)
	assert.Equal(t, l.bin, in.InstallDir)

	// A symlinked path to the recorded binary is the same program.
	link := filepath.Join(t.TempDir(), "vibe-link")
	require.NoError(t, os.Symlink(l.vibe, link))
	_, err = New("v0.1.1", l.opts(link)...).Detect()
	assert.NoError(t, err)

	// A manifest entry that is itself a symlink to the running binary.
	real := filepath.Join(t.TempDir(), "real-vibe")
	require.NoError(t, os.WriteFile(real, []byte("x"), 0o755))
	l2 := newLayout(t, false, "0.1.1")
	require.NoError(t, os.Remove(l2.vibe))
	require.NoError(t, os.Symlink(real, l2.vibe))
	_, err = New("v0.1.1", l2.opts(real)...).Detect()
	assert.NoError(t, err)

	// A different binary (go install, dev build) is external, even with a manifest present.
	other := filepath.Join(t.TempDir(), "vibe")
	require.NoError(t, os.WriteFile(other, []byte("other"), 0o755))
	_, err = New("v0.1.1", l.opts(other)...).Detect()
	assert.ErrorIs(t, err, ErrExternal)

	// No manifest at all.
	empty := newLayout(t, false, "0.1.1")
	require.NoError(t, os.Remove(empty.manifest))
	_, err = New("v0.1.1", empty.opts(empty.vibe)...).Detect()
	assert.ErrorIs(t, err, ErrExternal)
}

func TestCheck_ReportsExternal(t *testing.T) {
	f := newFakeGH(t)
	l := newLayout(t, false, "0.1.1")
	other := filepath.Join(t.TempDir(), "vibe")
	require.NoError(t, os.WriteFile(other, nil, 0o755))
	res, err := f.updater("v0.1.1", l.opts(other)...).Check(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, ManagedExternal, res.Managed)
	assert.False(t, res.Updatable)
	assert.Contains(t, res.ManagedNote, "update it the way you installed it")

	res, err = f.updater("v0.1.1", l.opts(l.vibe)...).Check(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, ManagedInstaller, res.Managed)
	assert.True(t, res.Updatable)
}

// ---------------------------------------------------------------- apply

func TestChecksumFor(t *testing.T) {
	a, b, c := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
	sums := a + "  install.sh.bak\n" + b + "  ./install.sh\n" + c + "  vibe_linux_amd64.tar.gz\n" + a + "  install.sh\n"
	got, err := checksumFor([]byte(sums), "install.sh")
	require.NoError(t, err)
	assert.Equal(t, a, got)

	got, err = checksumFor([]byte(a+" *install.sh\r\n"), "install.sh")
	require.NoError(t, err)
	assert.Equal(t, a, got, "binary-mode marker and CRLF")

	_, err = checksumFor([]byte(a+"  install.sh.bak\n"+b+"  ./install.sh\n"+b+"  Install.sh\n"), "install.sh")
	assert.Error(t, err, "decoys alone are not an entry")
	_, err = checksumFor([]byte(a+"  install.sh\n"+b+"  install.sh\n"), "install.sh")
	assert.Error(t, err, "two entries are ambiguous")
	_, err = checksumFor([]byte("nothex  install.sh\n"+"abc  install.sh\n"), "install.sh")
	assert.Error(t, err, "malformed hashes do not count")
}

// applyRig is a fake machine plus a fake release whose install.sh records how it was called.
type applyRig struct {
	f       *fakeGH
	l       layout
	out     string
	script  []byte
	baseEnv []string
}

func newApplyRig(t *testing.T, studio bool) *applyRig {
	t.Helper()
	r := &applyRig{f: newFakeGH(t), l: newLayout(t, studio, "0.1.1"), out: t.TempDir()}
	r.script = []byte(`#!/bin/sh
{
  for a in "$@"; do printf 'ARG:%s\n' "$a"; done
  printf 'ENV_VIBE_APP_DIR=%s\n' "${VIBE_APP_DIR:-}"
  printf 'ENV_VIBE_VERSION=%s\n' "${VIBE_VERSION:-}"
  printf 'ENV_VIBE_INSTALL_DIR=%s\n' "${VIBE_INSTALL_DIR:-}"
  printf 'ENV_VIBE_DOWNLOAD_BASE=%s\n' "${VIBE_DOWNLOAD_BASE:-}"
} > "$OUT/record"
echo "0.1.1 -> 0.1.2"
if [ -n "${WAIT_FOR:-}" ]; then while [ ! -e "$WAIT_FOR" ]; do sleep 0.05; done; fi
if [ -z "${NO_MANIFEST_BUMP:-}" ]; then sed -i.bak 's/^version=.*/version=0.1.2/' "$MANIFEST" && rm -f "$MANIFEST.bak"; fi
`)
	r.baseEnv = []string{"OUT=" + r.out, "MANIFEST=" + r.l.manifest, "PATH=" + os.Getenv("PATH"),
		"VIBE_VERSION=v9.9.9", "VIBE_INSTALL_DIR=/evil", "VIBE_DOWNLOAD_BASE=https://evil.example", "VIBE_APP_DIR=/evil-apps"}
	r.publish()
	return r
}

func (r *applyRig) publish() {
	r.f.mu.Lock()
	defer r.f.mu.Unlock()
	r.f.files["install.sh"] = r.script
	r.f.files["checksums.txt"] = []byte(sum(r.script) + "  install.sh\n" + strings.Repeat("0", 64) + "  vibe_linux_amd64.tar.gz\n")
}

func (r *applyRig) updater(extra ...string) *Updater {
	env := append(append([]string{}, r.baseEnv...), extra...)
	opts := append(r.l.opts(r.l.vibe), WithEnviron(func() []string { return env }))
	return r.f.updater("v0.1.1", opts...)
}

func (r *applyRig) record(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(r.out, "record"))
	require.NoError(t, err)
	return string(b)
}

func TestApply_RunsTheVerifiedScript(t *testing.T) {
	r := newApplyRig(t, true)
	res, err := r.updater().Apply(context.Background(), "v0.1.2")
	require.NoError(t, err)
	assert.Equal(t, "0.1.1", res.From)
	assert.Equal(t, "0.1.2", res.To)
	assert.Contains(t, res.Output, "0.1.1 -> 0.1.2")
	assert.Equal(t, filepath.Join(r.l.bin, "vibe-studio"), res.Restart)
	assert.True(t, res.Studio)

	rec := r.record(t)
	assert.Contains(t, rec, "ARG:--version\nARG:v0.1.2\nARG:--install-dir\nARG:"+r.l.bin+"\n")
	assert.NotContains(t, rec, "--force", "never --force")
	assert.NotContains(t, rec, "--cli-only", "studio was installed")
	assert.Contains(t, rec, "ENV_VIBE_VERSION=\n", "the caller's install.sh variables are scrubbed")
	assert.Contains(t, rec, "ENV_VIBE_INSTALL_DIR=\n")
	assert.Contains(t, rec, "ENV_VIBE_DOWNLOAD_BASE=\n")
	assert.Contains(t, rec, "ENV_VIBE_APP_DIR=\n")
	assert.NoFileExists(t, filepath.Join(r.l.data, "vibe", "update.lock"), "lock released")
}

func TestApply_CLIOnlyWhenStudioWasNotInstalled(t *testing.T) {
	r := newApplyRig(t, false)
	_, err := r.updater().Apply(context.Background(), "v0.1.2")
	require.NoError(t, err)
	assert.Contains(t, r.record(t), "ARG:--cli-only\n")
}

func TestApply_DevDownloadBaseIsPassedOnlyWhenConfigured(t *testing.T) {
	r := newApplyRig(t, false)
	u := r.updater()
	u.dlBase = "https://mirror.test/%s"
	_, err := u.Apply(context.Background(), "v0.1.2")
	require.NoError(t, err)
	assert.Contains(t, r.record(t), "ENV_VIBE_DOWNLOAD_BASE=https://mirror.test/v0.1.2\n")
}

func TestApply_RefusesBadChecksums(t *testing.T) {
	good := sum([]byte("whatever"))
	cases := map[string]string{
		"mismatch":   strings.Repeat("0", 64) + "  install.sh\n",
		"missing":    good + "  vibe_linux_amd64.tar.gz\n",
		"decoy bak":  "SUM  install.sh.bak\n",
		"decoy ./":   "SUM  ./install.sh\n",
		"empty file": "",
	}
	for name, sums := range cases {
		t.Run(name, func(t *testing.T) {
			r := newApplyRig(t, true)
			r.f.mu.Lock()
			r.f.files["checksums.txt"] = []byte(strings.ReplaceAll(sums, "SUM", sum(r.script)))
			r.f.mu.Unlock()
			_, err := r.updater().Apply(context.Background(), "v0.1.2")
			require.Error(t, err)
			assert.NoFileExists(t, filepath.Join(r.out, "record"), "the script must not run")
		})
	}
}

func TestApply_Caps(t *testing.T) {
	r := newApplyRig(t, true)
	r.f.mu.Lock()
	r.f.files["checksums.txt"] = []byte(strings.Repeat("# filler\n", maxChecksumsBytes/8))
	r.f.mu.Unlock()
	_, err := r.updater().Apply(context.Background(), "v0.1.2")
	assert.ErrorIs(t, err, ErrTooLarge)

	r = newApplyRig(t, true)
	r.script = []byte("#!/bin/sh\n# " + strings.Repeat("x", maxScriptBytes) + "\n")
	r.publish()
	_, err = r.updater().Apply(context.Background(), "v0.1.2")
	assert.ErrorIs(t, err, ErrTooLarge)
	assert.NoFileExists(t, filepath.Join(r.out, "record"))
}

func TestApply_Refusals(t *testing.T) {
	r := newApplyRig(t, true)
	ctx := context.Background()
	for _, tag := range []string{"", "latest", "v1.2", "1.2.3", "v1.2.3;id", "v1.2.3 --force", "../v1.2.3"} {
		_, err := r.updater().Apply(ctx, tag)
		assert.ErrorIs(t, err, ErrBadTag, tag)
	}
	for _, tag := range []string{"v0.1.1", "v0.1.0"} {
		_, err := r.updater().Apply(ctx, tag)
		assert.ErrorIs(t, err, ErrNotNewer, tag)
	}

	other := filepath.Join(t.TempDir(), "vibe")
	require.NoError(t, os.WriteFile(other, nil, 0o755))
	ext := r.f.updater("v0.1.1", r.l.opts(other)...)
	_, err := ext.Apply(ctx, "v0.1.2")
	assert.ErrorIs(t, err, ErrExternal)
	assert.NoFileExists(t, filepath.Join(r.out, "record"))
}

func TestApply_FailsWhenTheManifestDoesNotAdvance(t *testing.T) {
	r := newApplyRig(t, true)
	res, err := r.updater("NO_MANIFEST_BUMP=1").Apply(context.Background(), "v0.1.2")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "manifest says 0.1.1")
	assert.Contains(t, res.Output, "0.1.1 -> 0.1.2", "the output is still returned")
}

func TestApply_FailsWhenTheScriptFails(t *testing.T) {
	r := newApplyRig(t, true)
	r.script = []byte("#!/bin/sh\necho boom\nexit 3\n")
	r.publish()
	res, err := r.updater().Apply(context.Background(), "v0.1.2")
	require.Error(t, err)
	assert.Contains(t, res.Output, "boom")
}

func TestApply_OnlyOneAtATime(t *testing.T) {
	r := newApplyRig(t, true)
	gate := filepath.Join(r.out, "gate")
	done := make(chan error, 1)
	go func() {
		_, err := r.updater("WAIT_FOR="+gate).Apply(context.Background(), "v0.1.2")
		done <- err
	}()
	require.Eventually(t, func() bool { _, err := os.Stat(filepath.Join(r.out, "record")); return err == nil }, 10*time.Second, 20*time.Millisecond)

	_, err := r.updater().Apply(context.Background(), "v0.1.2")
	assert.ErrorIs(t, err, ErrApplyRunning)

	require.NoError(t, os.WriteFile(gate, nil, 0o644))
	require.NoError(t, <-done)
	_, err = r.updater().Apply(context.Background(), "v0.1.2")
	assert.NoError(t, err, "the lock is free again once the first finished")
}

func TestRelaunch(t *testing.T) {
	assert.Error(t, Relaunch("linux", "relative/vibe-studio"))
	assert.Error(t, Relaunch("linux", filepath.Join(t.TempDir(), "missing")))

	dir := t.TempDir()
	marker := filepath.Join(dir, "started")
	bin := filepath.Join(dir, "vibe-studio")
	require.NoError(t, os.WriteFile(bin, []byte("#!/bin/sh\necho \"$@\" > \""+marker+"\"\n"), 0o755))
	require.NoError(t, Relaunch("linux", bin, "/some/workspace"))
	require.Eventually(t, func() bool {
		b, err := os.ReadFile(marker)
		return err == nil && strings.TrimSpace(string(b)) == "/some/workspace"
	}, 5*time.Second, 20*time.Millisecond)
}

func TestServiceApply_TimesOutAndRemembersWhatItInstalled(t *testing.T) {
	r := newApplyRig(t, true)
	rec := prefs.NewRecents(prefs.NewStore(filepath.Join(t.TempDir(), "cfg")), nil)
	svc := NewService(r.updater("WAIT_FOR="+filepath.Join(r.out, "never")), rec, nil)
	svc.applyTimeout = 400 * time.Millisecond
	_, err := svc.Apply(context.Background(), "v0.1.2")
	require.Error(t, err, "a stalled install ends at the deadline")
	assert.Empty(t, svc.State().AppliedVersion)

	svc.applyTimeout = 10 * time.Second
	svc.u = r.updater()
	_, err = svc.Apply(context.Background(), "v0.1.2")
	require.NoError(t, err)
	st := svc.State()
	assert.Equal(t, "0.1.2", st.AppliedVersion, "the session remembers it until restart")
	assert.True(t, st.CanRestart)
}
