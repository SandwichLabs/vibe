// Package selfupdate tells a person that a newer Vibe release exists and, only
// when they confirm, upgrades by running that release's own install.sh.
//
// Nothing here runs in the background on its own: the callers (Studio's startup
// check, `vibe update`, Settings > About) decide when to look, and the person
// decides whether to install. The one network surface is plain HTTPS GETs to
// github.com (the latest-release redirect, install.sh and checksums.txt) and
// api.github.com (release notes, best effort), with a fixed User-Agent and no
// cookies, tokens, identifiers or workspace data (docs/privacy.md).
//
// Only a Vibe that the installer put there is updatable: the installer's
// manifest (see manifest.go) must list the running executable. Anything else
// (a dev build, `go install`, a package manager) is told to update the way it
// was installed.
package selfupdate

import (
	"errors"
	"net/http"
	"os"
	"runtime"
	"strings"
)

const (
	// Repo is the GitHub repository releases are read from.
	Repo = "SandwichLabs/vibe"

	defaultWebBase = "https://github.com"
	defaultAPIBase = "https://api.github.com"

	userAgent = "vibe-update"

	// Dev-only QA hooks. They are read only when the binary's version is "dev"
	// (a release build ignores both), so a release can never be pointed at a
	// different server by its environment.
	envBaseURL        = "VIBE_UPDATE_BASE_URL"
	envPretendVersion = "VIBE_UPDATE_PRETEND_VERSION"
)

// Managed says who installed the running Vibe.
const (
	ManagedInstaller = "installer" // install.sh put it there: updatable
	ManagedExternal  = "external"  // anything else: notify only
)

// ExternalNote is shown when the running Vibe is not an installer-managed one.
const ExternalNote = "Installed outside the Vibe installer (dev build, `go install`, or a package manager); update it the way you installed it."

// Errors callers can match.
var (
	ErrExternal     = errors.New("this Vibe was not installed by the Vibe installer; update it the way you installed it")
	ErrApplyRunning = errors.New("an update is already running")
	ErrBadTag       = errors.New("not a release version")
	ErrNotNewer     = errors.New("that version is not newer than this one")
)

// Updater checks for and applies releases. Build one with New.
type Updater struct {
	version  string // the running binary's version as stamped (may be "dev")
	current  string // what to compare against (version, or the dev-hook pretend value)
	webBase  string
	apiBase  string
	dlBase   string // when set, handed to install.sh as VIBE_DOWNLOAD_BASE (tests, dev hook)
	insecure bool   // allow http:// (tests and the dev hook only)
	hosts    map[string]bool
	client   *http.Client
	getenv   func(string) string
	environ  func() []string
	goos     string
	home     func() (string, error)
	exe      func() (string, error)
}

// Option configures an Updater.
type Option func(*Updater)

// WithHTTPClient sets the transport underneath every request (tests inject an
// httptest TLS client). Timeouts and redirect rules are applied on top.
func WithHTTPClient(c *http.Client) Option { return func(u *Updater) { u.client = c } }

// WithBaseURLs points the release site and the API at other servers.
func WithBaseURLs(web, api string) Option {
	return func(u *Updater) { u.webBase, u.apiBase = strings.TrimRight(web, "/"), strings.TrimRight(api, "/") }
}

// WithAllowedHosts adds hosts a redirect may land on.
func WithAllowedHosts(hosts ...string) Option {
	return func(u *Updater) {
		for _, h := range hosts {
			u.hosts[h] = true
		}
	}
}

// WithInsecure allows http:// URLs. Tests and the dev hook only.
func WithInsecure() Option { return func(u *Updater) { u.insecure = true } }

// WithGetenv replaces the environment lookup (XDG_DATA_HOME, the dev hooks).
func WithGetenv(f func(string) string) Option { return func(u *Updater) { u.getenv = f } }

// WithEnviron replaces os.Environ (the base environment install.sh runs in).
func WithEnviron(f func() []string) Option { return func(u *Updater) { u.environ = f } }

// WithPlatform sets the GOOS used to place the manifest.
func WithPlatform(goos string) Option { return func(u *Updater) { u.goos = goos } }

// WithHomeDir replaces os.UserHomeDir.
func WithHomeDir(f func() (string, error)) Option { return func(u *Updater) { u.home = f } }

// WithExecutable replaces os.Executable.
func WithExecutable(f func() (string, error)) Option { return func(u *Updater) { u.exe = f } }

// WithDownloadBase tells install.sh where release assets live (VIBE_DOWNLOAD_BASE),
// as a template: "%s" is replaced by the tag. Tests and the dev hook only.
func WithDownloadBase(tmpl string) Option { return func(u *Updater) { u.dlBase = tmpl } }

// New returns an Updater for the binary stamped with version. getenv defaults
// to os.Getenv. When version is "dev" the two QA hooks are honoured.
func New(version string, opts ...Option) *Updater {
	u := &Updater{
		version: version,
		current: version,
		webBase: defaultWebBase,
		apiBase: defaultAPIBase,
		hosts:   map[string]bool{"github.com": true, "objects.githubusercontent.com": true, "release-assets.githubusercontent.com": true},
		client:  &http.Client{},
		getenv:  os.Getenv,
		environ: os.Environ,
		goos:    runtime.GOOS,
		home:    os.UserHomeDir,
		exe:     os.Executable,
	}
	for _, o := range opts {
		o(u)
	}
	if version == "dev" {
		u.applyDevHooks()
	}
	return u
}

// applyDevHooks reads the QA environment. Called only for a "dev" binary.
func (u *Updater) applyDevHooks() {
	if b := strings.TrimRight(strings.TrimSpace(u.getenv(envBaseURL)), "/"); b != "" {
		u.webBase, u.apiBase, u.insecure = b, b+"/api", true
		u.dlBase = b + "/" + Repo + "/releases/download/%s"
	}
	if p := strings.TrimSpace(u.getenv(envPretendVersion)); p != "" {
		u.current = p
	}
}

// Version is the version the running binary was stamped with.
func (u *Updater) Version() string { return u.version }

// IsDev reports whether this is an unstamped development build.
func (u *Updater) IsDev() bool { return u.version == "dev" }

// ErrNothingToRestart means no update was installed in this session.
var ErrNothingToRestart = errors.New("no update was installed in this session")
