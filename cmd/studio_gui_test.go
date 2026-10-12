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
			exe:      "/opt/vibe/bin/vibe",
			files:    []string{"/opt/vibe/bin/vibe-studio"},
			target:   target,
			wantPath: "/opt/vibe/bin/vibe-studio",
			wantArgv: []string{"vibe-studio", target},
		},
		{
			name:     "a symlinked vibe finds the neighbour of its real file",
			goos:     "linux",
			exe:      "/usr/local/bin/vibe",
			links:    map[string]string{"/usr/local/bin/vibe": "/opt/vibe/bin/vibe"},
			files:    []string{"/opt/vibe/bin/vibe-studio"},
			wantPath: "/opt/vibe/bin/vibe-studio",
			wantArgv: []string{"vibe-studio"},
		},
		{
			name:     "symlink resolution fails: the unresolved path is used",
			goos:     "linux",
			exe:      "/opt/vibe/bin/vibe",
			linkErr:  true,
			files:    []string{"/opt/vibe/bin/vibe-studio"},
			wantPath: "/opt/vibe/bin/vibe-studio",
			wantArgv: []string{"vibe-studio"},
		},
		{
			name:    "a directory named vibe-studio is not the binary",
			goos:    "linux",
			exe:     "/opt/vibe/bin/vibe",
			dirNote: true,
			files:   []string{"/opt/vibe/bin/vibe-studio"},
			wantErr: []string{"$PATH"},
		},
		{
			name:     "falls back to PATH",
			goos:     "linux",
			exe:      "/opt/vibe/bin/vibe",
			path:     map[string]string{"vibe-studio": "/usr/bin/vibe-studio"},
			wantPath: "/usr/bin/vibe-studio",
			wantArgv: []string{"vibe-studio"},
		},
		{
			name:     "macOS: app beside the CLI uses open -a",
			goos:     "darwin",
			exe:      "/Users/me/dl/vibe",
			files:    []string{"/Users/me/dl/Vibe Studio.app"},
			target:   target,
			wantPath: "/usr/bin/open",
			wantArgv: []string{"open", "-a", "/Users/me/dl/Vibe Studio.app", target},
		},
		{
			name:     "macOS: ~/Applications, no argument",
			goos:     "darwin",
			exe:      "/Users/me/.local/bin/vibe",
			files:    []string{"/Users/me/Applications/Vibe Studio.app"},
			wantPath: "/usr/bin/open",
			wantArgv: []string{"open", "-a", "/Users/me/Applications/Vibe Studio.app"},
		},
		{
			name:     "macOS: /Applications last",
			goos:     "darwin",
			exe:      "/Users/me/.local/bin/vibe",
			files:    []string{"/Applications/Vibe Studio.app"},
			wantPath: "/usr/bin/open",
			wantArgv: []string{"open", "-a", "/Applications/Vibe Studio.app"},
		},
		{
			name:     "macOS: a binary beside the CLI wins over an app",
			goos:     "darwin",
			exe:      "/Users/me/.local/bin/vibe",
			files:    []string{"/Users/me/.local/bin/vibe-studio", "/Applications/Vibe Studio.app"},
			wantPath: "/Users/me/.local/bin/vibe-studio",
			wantArgv: []string{"vibe-studio"},
		},
		{
			name:     "linux never looks for an app bundle",
			goos:     "linux",
			exe:      "/opt/vibe/bin/vibe",
			files:    []string{"/Applications/Vibe Studio.app"},
			wantErr:  []string{"/opt/vibe/bin/vibe-studio", "$PATH", "terminal UI"},
			wantArgv: nil,
		},
		{
			name:    "not found lists what was searched",
			goos:    "darwin",
			exe:     "/Users/me/bin/vibe",
			wantErr: []string{"/Users/me/bin/vibe-studio", "$PATH", "/Users/me/Applications/Vibe Studio.app", "/Applications/Vibe Studio.app"},
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
		lookPath:     func(string) (string, error) { return "/bin/vibe-studio", nil },
	}
	got, err := l.find("")
	require.NoError(t, err)
	assert.Equal(t, "/bin/vibe-studio", got.path)
}
