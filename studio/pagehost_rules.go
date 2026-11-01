//go:build !windows

package main

// The macOS helper keeps its compiled content-rule list in a throwaway folder,
// slaw-rules-<pid>, in a temporary directory. Studio ends a helper by killing its
// process group, so the helper's own cleanup at exit never runs (found in the macOS
// owner QA: 71 folders in one session). Two things remove them: Studio, after it
// reaps a helper, and the next helper, at start. Both only ever touch a folder of
// this exact name pattern, directly in that one directory, never through a link.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const (
	rulesDirPrefix = "slaw-rules-"
	// rulesParentEnv is the one variable Studio gives the macOS helper: the
	// directory to make the folder in, so both sides name the same place.
	rulesParentEnv = "SLAW_RULES_PARENT"
)

// cleanAbs is p cleaned, when it is an absolute path with no ".." element
// (a trailing slash is fine: macOS's $TMPDIR ends in one).
func cleanAbs(p string) (string, bool) {
	if p == "" || !filepath.IsAbs(p) {
		return "", false
	}
	for _, el := range strings.Split(p, "/") {
		if el == ".." {
			return "", false
		}
	}
	return filepath.Clean(p), true
}

// rulesParentFor is the directory the folders live in for a process given getenv:
// the value Studio passed. ok is false when there is none, in which case the macOS
// helper keeps its folder in its own per-user temporary directory and nothing sweeps
// or removes by path (a shared /tmp is never touched).
func rulesParentFor(getenv func(string) string) (string, bool) {
	return cleanAbs(getenv(rulesParentEnv))
}

// studioRulesParent is the directory Studio gives the helper: its own $TMPDIR, if
// it has a usable one.
func studioRulesParent() (string, bool) { return cleanAbs(os.Getenv("TMPDIR")) }

// rulesDirName is the folder's name for a helper's pid.
func rulesDirName(pid int) string { return rulesDirPrefix + strconv.Itoa(pid) }

// parseRulesDirName returns the pid in a folder name of exactly the pattern, and
// false for anything else (no sign, no leading zero, no suffix).
func parseRulesDirName(name string) (int, bool) {
	digits, ok := strings.CutPrefix(name, rulesDirPrefix)
	if !ok || digits == "" || len(digits) > 10 {
		return 0, false
	}
	pid, err := strconv.Atoi(digits)
	if err != nil || pid <= 0 || strconv.Itoa(pid) != digits {
		return 0, false
	}
	return pid, true
}

// isRealDir says path is a directory itself, not a link to one, and ours.
func isRealDir(path string) bool {
	fi, err := os.Lstat(path)
	if err != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		return false
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Getuid() {
		return false
	}
	return true
}

// removeRulesDir removes the folder of one helper, if it is there. The path is
// built from Studio's own directory and the pid, never from anything the helper sent.
func removeRulesDir(parent string, pid int) {
	if pid <= 0 {
		return
	}
	if p := filepath.Join(parent, rulesDirName(pid)); isRealDir(p) {
		_ = os.RemoveAll(p)
	}
}

// pidAlive says a process with this pid exists (signal 0 checks, sends nothing).
func pidAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// sweepStaleRulesDirs removes the folders in parent whose pid is no longer alive.
// A folder that is not of the pattern, a link, or another user's is left alone, and
// so is a folder of a process that still runs (another window's helper).
func sweepStaleRulesDirs(parent string, self int, alive func(int) bool) {
	ents, err := os.ReadDir(parent)
	if err != nil {
		return
	}
	for _, e := range ents {
		pid, ok := parseRulesDirName(e.Name())
		if !ok || pid == self || alive(pid) {
			continue
		}
		if p := filepath.Join(parent, e.Name()); isRealDir(p) {
			_ = os.RemoveAll(p)
		}
	}
}
