package hub

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/SandwichLabs/slaw/internal/filestore"
)

// ClaudeConfig is what "Copy Claude Desktop config" and "Copy Claude Code
// config" put on the clipboard for a workspace.
//
// Neither snippet ever contains a token: they launch `slaw mcp serve
// --workspace DIR`, which is stdio, and when Studio owns the workspace the
// process proxies to Studio and reads the token from hub.lock at run time.
type ClaudeConfig struct {
	// Name is the MCP server name used in both snippets, "slaw-<folder>".
	Name string `json:"name"`
	// Desktop is JSON for claude_desktop_config.json (a "mcpServers" object).
	Desktop string `json:"desktop"`
	// Code is the shell command that registers the server with Claude Code.
	Code string `json:"code"`
}

// BuildClaudeConfig renders the snippets for the workspace folder root, to be
// launched through the slaw binary at exe (an absolute path; see LocateSlaw).
func BuildClaudeConfig(root, exe string) (ClaudeConfig, error) {
	if !filepath.IsAbs(root) || !filepath.IsAbs(exe) {
		return ClaudeConfig{}, errors.New("the workspace folder and the slaw binary need absolute paths")
	}
	name := "slaw-" + slug(filepath.Base(root))
	args := []string{"mcp", "serve", "--workspace", root}
	desktop, err := json.MarshalIndent(map[string]any{
		"mcpServers": map[string]any{
			name: map[string]any{"command": exe, "args": args},
		},
	}, "", "  ")
	if err != nil {
		return ClaudeConfig{}, fmt.Errorf("failed to render the Claude Desktop config: %w", err)
	}
	parts := []string{"claude", "mcp", "add", name, "--", shellQuote(exe)}
	for _, a := range args {
		parts = append(parts, shellQuote(a))
	}
	return ClaudeConfig{Name: name, Desktop: string(desktop), Code: strings.Join(parts, " ")}, nil
}

// LocateSlaw finds the `slaw` CLI an MCP client should launch: the running
// binary when it is the CLI, else a `slaw` beside it (Studio ships next to the
// CLI), else one on PATH. The result is absolute with symlinks left alone so a
// package-manager shim keeps working.
func LocateSlaw() (string, error) {
	self, err := os.Executable()
	if err == nil {
		if base := filepath.Base(self); base == "slaw" || base == "slaw.exe" {
			return self, nil
		}
		for _, n := range []string{"slaw", "slaw.exe"} {
			cand := filepath.Join(filepath.Dir(self), n)
			if fi, serr := os.Stat(cand); serr == nil && fi.Mode().IsRegular() {
				return cand, nil
			}
		}
	}
	if p, lerr := exec.LookPath("slaw"); lerr == nil {
		if abs, aerr := filepath.Abs(p); aerr == nil {
			return abs, nil
		}
	}
	return "", errors.New("could not find the slaw command-line tool; install it and make sure it is on your PATH")
}

func slug(s string) string { return filestore.SlugOr(s, "workspace", 0) }

// shellQuote quotes s for a POSIX shell only when it needs it.
func shellQuote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/._-+=:@%", r))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
