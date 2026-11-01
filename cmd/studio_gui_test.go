package cmd

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGUILauncherFind(t *testing.T) {
	const target = "/work/space"
	tests := []struct {
		name     string
		goos     string
		exe      string
		links    map[string]string // evalSymlinks results
		dirNote  bool              // the listed files are directories
		linkErr  bool              // evalSymlinks fails
		files    []string
		path     map[string]string // lookPath results
		target   string
		wantPath string
		wantArgv []string
		wantErr  []string
	}{
		{
			name:     "beside the executable",
			goos:     "linux",
			exe:      "/opt/slaw/bin/slaw",
			files:    []string{"/opt/slaw/bin/slaw-studio"},
			target:   target,
			wantPath: "/opt/slaw/bin/slaw-studio",
			wantArgv: []string{"slaw-studio", target},
		},
		{
			name:     "a symlinked slaw finds the neighbour of its real file",
			goos:     "linux",
			exe:      "/usr/local/bin/slaw",
			links:    map[string]string{"/usr/local/bin/slaw": "/opt/slaw/bin/slaw"},
			files:    []string{"/opt/slaw/bin/slaw-studio"},
			wantPath: "/opt/slaw/bin/slaw-studio",
			wantArgv: []string{"slaw-studio"},
		},
		{
			name:     "symlink resolution fails: the unresolved path is used",
			goos:     "linux",
			exe:      "/opt/slaw/bin/slaw",
			linkErr:  true,
			files:    []string{"/opt/slaw/bin/slaw-studio"},
			wantPath: "/opt/slaw/bin/slaw-studio",
			wantArgv: []string{"slaw-studio"},
		},
		{
			name:    "a directory named slaw-studio is not the binary",
			goos:    "linux",
			exe:     "/opt/slaw/bin/slaw",
			dirNote: true,
			files:   []string{"/opt/slaw/bin/slaw-studio"},
			wantErr: []string{"$PATH"},
		},
		{
			name:     "falls back to PATH",
			goos:     "linux",
			exe:      "/opt/slaw/bin/slaw",
			path:     map[string]string{"slaw-studio": "/usr/bin/slaw-studio"},
			wantPath: "/usr/bin/slaw-studio",
			wantArgv: []string{"slaw-studio"},
		},
		{
			name:     "macOS: app beside the CLI uses open -a",
			goos:     "darwin",
			exe:      "/Users/me/dl/slaw",
			files:    []string{"/Users/me/dl/Slaw.app"},
			target:   target,
			wantPath: "/usr/bin/open",
			wantArgv: []string{"open", "-a", "/Users/me/dl/Slaw.app", target},
		},
		{
			name:     "macOS: ~/Applications, no argument",
			goos:     "darwin",
			exe:      "/Users/me/.local/bin/slaw",
			files:    []string{"/Users/me/Applications/Slaw.app"},
			wantPath: "/usr/bin/open",
			wantArgv: []string{"open", "-a", "/Users/me/Applications/Slaw.app"},
		},
		{
			name:     "macOS: /Applications last",
			goos:     "darwin",
			exe:      "/Users/me/.local/bin/slaw",
			files:    []string{"/Applications/Slaw.app"},
			wantPath: "/usr/bin/open",
			wantArgv: []string{"open", "-a", "/Applications/Slaw.app"},
		},
		{
			name:     "macOS: a binary beside the CLI wins over an app",
			goos:     "darwin",
			exe:      "/Users/me/.local/bin/slaw",
			files:    []string{"/Users/me/.local/bin/slaw-studio", "/Applications/Slaw.app"},
			wantPath: "/Users/me/.local/bin/slaw-studio",
			wantArgv: []string{"slaw-studio"},
		},
		{
			name:     "linux never looks for an app bundle",
			goos:     "linux",
			exe:      "/opt/slaw/bin/slaw",
			files:    []string{"/Applications/Slaw.app"},
			wantErr:  []string{"/opt/slaw/bin/slaw-studio", "$PATH", "terminal UI"},
			wantArgv: nil,
		},
		{
			name:    "not found lists what was searched",
			goos:    "darwin",
			exe:     "/Users/me/bin/slaw",
			wantErr: []string{"/Users/me/bin/slaw-studio", "$PATH", "/Users/me/Applications/Slaw.app", "/Applications/Slaw.app"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]bool{}
			dirs := map[string]bool{}
			for _, f := range tc.files {
				files[f] = true
				if tc.dirNote || strings.HasSuffix(f, ".app") {
					dirs[f] = true
				}
			}
			l := guiLauncher{
				goos:       tc.goos,
				home:       "/Users/me",
				executable: func() (string, error) { return tc.exe, nil },
				evalSymlinks: func(p string) (string, error) {
					if tc.linkErr {
						return "", errors.New("broken link")
					}
					if to, ok := tc.links[p]; ok {
						return to, nil
					}
					return p, nil
				},
				isFile: func(p string) bool { return files[p] && !dirs[p] },
				isDir:  func(p string) bool { return files[p] && dirs[p] },
				lookPath: func(n string) (string, error) {
					if p, ok := tc.path[n]; ok {
						return p, nil
					}
					return "", errors.New("not found")
				},
			}
			got, err := l.find(tc.target)
			if tc.wantErr != nil {
				require.Error(t, err)
				for _, s := range tc.wantErr {
					assert.Contains(t, err.Error(), filepath.FromSlash(s))
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, filepath.FromSlash(tc.wantPath), got.path)
			assert.Equal(t, tc.wantArgv, got.argv)
		})
	}
}

func TestGUILauncherUnknownExecutable(t *testing.T) {
	l := guiLauncher{
		goos:         "linux",
		executable:   func() (string, error) { return "", errors.New("no exe") },
		evalSymlinks: func(p string) (string, error) { return p, nil },
		isFile:       func(string) bool { return false },
		isDir:        func(string) bool { return false },
		lookPath:     func(string) (string, error) { return "/bin/slaw-studio", nil },
	}
	got, err := l.find("")
	require.NoError(t, err)
	assert.Equal(t, "/bin/slaw-studio", got.path)
}
