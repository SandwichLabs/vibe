package selfupdate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/mod/semver"

	"github.com/SandwichLabs/vibe/internal/filelock"
)

// maxScriptOutput caps the install output kept for display.
const maxScriptOutput = 64 << 10

// ApplyResult is what a finished upgrade reports.
type ApplyResult struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Output  string `json:"output"`
	Studio  bool   `json:"studio"`  // the desktop app was part of the install
	Restart string `json:"restart"` // how to start the new Studio ("" when it is not installed)
}

var applyMu sync.Mutex

// scriptEnvScrub lists the variables install.sh reads that must not leak in from the caller's
// environment: the command line (or our own value) is the only source for each.
func scriptEnvScrub() []string {
	return []string{"VIBE_VERSION", "VIBE_INSTALL_DIR", "INSTALL_DIR", "VIBE_APP_DIR", "VIBE_DOWNLOAD_BASE", "VIBE_LATEST_URL"}
}

// scriptWaitDelay bounds how long after a cancelled install the script's output pipes are waited on.
const scriptWaitDelay = 5 * time.Second

// Apply upgrades to tag by downloading that release's install.sh, verifying it
// against the release's checksums.txt and running it. It refuses when this
// Vibe is not installer-managed, when tag is not a newer release, and while
// another upgrade runs (in this process or another). It never passes --force.
func (u *Updater) Apply(ctx context.Context, tag string) (ApplyResult, error) {
	if !ValidTag(tag) {
		return ApplyResult{}, fmt.Errorf("%w: %q", ErrBadTag, tag)
	}
	if cur := normalizeVersion(u.current); cur != "" && semver.Compare(tag, cur) <= 0 {
		return ApplyResult{}, fmt.Errorf("%w (%s, running %s)", ErrNotNewer, tag, u.current)
	}
	in, err := u.Detect()
	if err != nil {
		return ApplyResult{}, err
	}
	release, err := lockApply(in)
	if err != nil {
		return ApplyResult{}, err
	}
	defer release()

	script, err := u.fetchVerifiedScript(ctx, tag)
	if err != nil {
		return ApplyResult{}, err
	}
	output, runErr := u.runScript(ctx, script, tag, in)
	res := ApplyResult{From: in.Version, To: strings.TrimPrefix(tag, "v"), Output: output, Studio: in.Studio}
	if runErr != nil {
		return res, fmt.Errorf("the installer failed: %w", runErr)
	}
	after, err := ReadManifest(in.ManifestPath)
	if err != nil || after.Version != strings.TrimPrefix(tag, "v") {
		got := "(unreadable)"
		if err == nil {
			got = after.Version
		}
		return res, fmt.Errorf("the installer finished but the install manifest says %s, not %s", got, strings.TrimPrefix(tag, "v"))
	}
	if after.Studio {
		res.Restart = after.StudioPath
	}
	return res, nil
}

// lockApply serialises upgrades: a mutex in this process and a lock file beside the manifest across processes.
func lockApply(in *Install) (release func(), err error) {
	if !applyMu.TryLock() {
		return nil, ErrApplyRunning
	}
	dir := filepath.Dir(in.ManifestPath)
	d, err := os.Open(dir)
	if err != nil {
		applyMu.Unlock()
		return nil, fmt.Errorf("failed to open %s: %w", dir, err)
	}
	l, err := filelock.AcquireAt(d, "update.lock", []byte(fmt.Sprintf("%d\n", os.Getpid())))
	if err != nil {
		_ = d.Close()
		applyMu.Unlock()
		if errors.Is(err, filelock.ErrHeld) {
			return nil, ErrApplyRunning
		}
		return nil, fmt.Errorf("failed to take the update lock: %w", err)
	}
	return func() { _ = l.Release(); _ = d.Close(); applyMu.Unlock() }, nil
}

// fetchVerifiedScript downloads install.sh and checksums.txt for tag and returns the script
// only if its sha256 equals the checksum of the entry named exactly "install.sh".
func (u *Updater) fetchVerifiedScript(ctx context.Context, tag string) ([]byte, error) {
	base := u.webBase + "/" + Repo + "/releases/download/" + tag
	sums, err := u.getOK(ctx, base+"/checksums.txt", maxChecksumsBytes, downloadTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to download checksums.txt: %w", err)
	}
	want, err := checksumFor(sums, "install.sh")
	if err != nil {
		return nil, err
	}
	script, err := u.getOK(ctx, base+"/install.sh", maxScriptBytes, downloadTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to download install.sh: %w", err)
	}
	got := sha256.Sum256(script)
	if hex.EncodeToString(got[:]) != want {
		return nil, errors.New("install.sh does not match the release's checksums.txt; refusing to run it")
	}
	return script, nil
}

// checksumFor finds the sha256 of exactly the file called name in a `sha256sum`-style list.
// A decoy such as "install.sh.bak" or "./install.sh" never matches; more than one entry for
// the name is ambiguous and refused.
func checksumFor(sums []byte, name string) (string, error) {
	var found []string
	for _, line := range strings.Split(string(sums), "\n") {
		line = strings.TrimRight(line, "\r")
		sum, rest, ok := strings.Cut(line, " ")
		if !ok || len(sum) != 64 {
			continue
		}
		if _, err := hex.DecodeString(sum); err != nil {
			continue
		}
		file := strings.TrimPrefix(strings.TrimPrefix(rest, " "), "*") // text or binary mode marker
		if file == name {
			found = append(found, strings.ToLower(sum))
		}
	}
	switch len(found) {
	case 0:
		return "", fmt.Errorf("checksums.txt has no entry for %s; refusing to run it", name)
	case 1:
		return found[0], nil
	}
	return "", fmt.Errorf("checksums.txt lists %s more than once; refusing to run it", name)
}

// runScript writes the script to a fresh private temp dir and runs it with /bin/sh. Never --force,
// never a shell string: an argv slice.
func (u *Updater) runScript(ctx context.Context, script []byte, tag string, in *Install) (string, error) {
	dir, err := os.MkdirTemp("", "vibe-update-")
	if err != nil {
		return "", fmt.Errorf("failed to create a temp dir: %w", err)
	}
	defer os.RemoveAll(dir)
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", fmt.Errorf("failed to secure the temp dir: %w", err)
	}
	path := filepath.Join(dir, "install.sh")
	if err := os.WriteFile(path, script, 0o600); err != nil {
		return "", fmt.Errorf("failed to write install.sh: %w", err)
	}
	args := []string{path, "--version", tag, "--install-dir", in.InstallDir}
	if !in.Studio {
		args = append(args, "--cli-only")
	}
	cmd := exec.CommandContext(ctx, "/bin/sh", args...)
	cmd.Env = u.scriptEnv(tag, in)
	cmd.Dir = dir
	cmd.WaitDelay = scriptWaitDelay
	ownProcessGroup(cmd)
	var buf cappedBuffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err = cmd.Run()
	return buf.String(), err
}

// scriptEnv is the caller's environment minus the variables install.sh reads, plus our own values.
func (u *Updater) scriptEnv(tag string, in *Install) []string {
	drop := map[string]bool{}
	for _, k := range scriptEnvScrub() {
		drop[k] = true
	}
	var env []string
	for _, kv := range u.environ() {
		k, _, _ := strings.Cut(kv, "=")
		if !drop[k] {
			env = append(env, kv)
		}
	}
	env = append(env, "NO_COLOR=1")
	if in.AppDir != "" {
		env = append(env, "VIBE_APP_DIR="+in.AppDir)
	}
	if u.dlBase != "" {
		env = append(env, "VIBE_DOWNLOAD_BASE="+strings.ReplaceAll(u.dlBase, "%s", tag))
	}
	return env
}

// cappedBuffer keeps at most maxScriptOutput bytes (the head) and drops the rest.
type cappedBuffer struct{ b bytes.Buffer }

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if room := maxScriptOutput - c.b.Len(); room > 0 {
		if len(p) > room {
			c.b.Write(p[:room])
		} else {
			c.b.Write(p)
		}
	}
	return len(p), nil
}

func (c *cappedBuffer) String() string { return sanitizeText(c.b.String(), maxScriptOutput) }
