package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GitAvailable reports whether git is on PATH (workspace init can then offer to
// make the folder a repository).
func GitAvailable() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// GitInfo reads the branch from .git/HEAD without running git: a repository's
// own config (core.fsmonitor, filters) is never executed on open, so opening a
// folder cannot run a program the folder names. isRepo is false when root has no
// .git; branch is empty for an unreadable HEAD, and the first seven characters
// of the commit for a detached HEAD.
func GitInfo(root string) (branch string, isRepo bool) {
	gitPath := filepath.Join(root, ".git")
	fi, err := os.Stat(gitPath)
	if err != nil {
		return "", false
	}
	headDir := gitPath
	if !fi.IsDir() { // a worktree or submodule: ".git" is a file "gitdir: <path>"
		b, err := os.ReadFile(gitPath)
		if err != nil {
			return "", true
		}
		line := strings.TrimSpace(string(b))
		if !strings.HasPrefix(line, "gitdir:") {
			return "", true
		}
		headDir = strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
		if !filepath.IsAbs(headDir) {
			headDir = filepath.Join(root, headDir)
		}
	}
	b, err := os.ReadFile(filepath.Join(headDir, "HEAD"))
	if err != nil {
		return "", true
	}
	head := strings.TrimSpace(string(b))
	if strings.HasPrefix(head, "ref:") {
		ref := strings.TrimSpace(strings.TrimPrefix(head, "ref:"))
		return strings.TrimPrefix(ref, "refs/heads/"), true
	}
	if len(head) > 7 {
		head = head[:7]
	}
	return head, true
}

// HasConfigFile reports whether dir holds a regular vibe.yaml (not a symlink):
// the mark of a workspace folder.
func HasConfigFile(dir string) bool {
	fi, err := os.Lstat(filepath.Join(dir, ConfigFile))
	return err == nil && fi.Mode().IsRegular()
}
