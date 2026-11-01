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
			exe:      "/opt/assay/bin/assay",
			files:    []string{"/opt/assay/bin/assay-studio"},
			target:   target,
			wantPath: "/opt/assay/bin/assay-studio",
			wantArgv: []string{"assay-studio", target},
		},
		{
			name:     "a symlinked assay finds the neighbour of its real file",
			goos:     "linux",
			exe:      "/usr/local/bin/assay",
			links:    map[string]string{"/usr/local/bin/assay": "/opt/assay/bin/assay"},
			files:    []string{"/opt/assay/bin/assay-studio"},
			wantPath: "/opt/assay/bin/assay-studio",
			wantArgv: []string{"assay-studio"},
		},
		{
			name:     "symlink resolution fails: the unresolved path is used",
			goos:     "linux",
			exe:      "/opt/assay/bin/assay",
			linkErr:  true,
			files:    []string{"/opt/assay/bin/assay-studio"},
			wantPath: "/opt/assay/bin/assay-studio",
			wantArgv: []string{"assay-studio"},
		},
		{
			name:    "a directory named assay-studio is not the binary",
			goos:    "linux",
			exe:     "/opt/assay/bin/assay",
			dirNote: true,
			files:   []string{"/opt/assay/bin/assay-studio"},
			wantErr: []string{"$PATH"},
		},
		{
			name:     "falls back to PATH",
			goos:     "linux",
			exe:      "/opt/assay/bin/assay",
			path:     map[string]string{"assay-studio": "/usr/bin/assay-studio"},
			wantPath: "/usr/bin/assay-studio",
			wantArgv: []string{"assay-studio"},
		},
		{
			name:     "macOS: app beside the CLI uses open -a",
			goos:     "darwin",
			exe:      "/Users/me/dl/assay",
			files:    []string{"/Users/me/dl/Assay.app"},
			target:   target,
			wantPath: "/usr/bin/open",
			wantArgv: []string{"open", "-a", "/Users/me/dl/Assay.app", target},
		},
		{
			name:     "macOS: ~/Applications, no argument",
			goos:     "darwin",
			exe:      "/Users/me/.local/bin/assay",
			files:    []string{"/Users/me/Applications/Assay.app"},
			wantPath: "/usr/bin/open",
			wantArgv: []string{"open", "-a", "/Users/me/Applications/Assay.app"},
		},
		{
			name:     "macOS: /Applications last",
			goos:     "darwin",
			exe:      "/Users/me/.local/bin/assay",
			files:    []string{"/Applications/Assay.app"},
			wantPath: "/usr/bin/open",
			wantArgv: []string{"open", "-a", "/Applications/Assay.app"},
		},
		{
			name:     "macOS: a binary beside the CLI wins over an app",
			goos:     "darwin",
			exe:      "/Users/me/.local/bin/assay",
			files:    []string{"/Users/me/.local/bin/assay-studio", "/Applications/Assay.app"},
			wantPath: "/Users/me/.local/bin/assay-studio",
			wantArgv: []string{"assay-studio"},
		},
		{
			name:     "linux never looks for an app bundle",
			goos:     "linux",
			exe:      "/opt/assay/bin/assay",
			files:    []string{"/Applications/Assay.app"},
			wantErr:  []string{"/opt/assay/bin/assay-studio", "$PATH", "terminal UI"},
			wantArgv: nil,
		},
		{
			name:    "not found lists what was searched",
			goos:    "darwin",
			exe:     "/Users/me/bin/assay",
			wantErr: []string{"/Users/me/bin/assay-studio", "$PATH", "/Users/me/Applications/Assay.app", "/Applications/Assay.app"},
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
		lookPath:     func(string) (string, error) { return "/bin/assay-studio", nil },
	}
	got, err := l.find("")
	require.NoError(t, err)
	assert.Equal(t, "/bin/assay-studio", got.path)
}
