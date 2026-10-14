package selfupdate

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Install is what install.sh recorded about the Vibe it put on this machine.
type Install struct {
	ManifestPath string
	// Version is the manifest's "version=" line without a leading "v" (install.sh writes 0.1.2).
	Version string
	// InstallDir is the directory of the recorded `vibe` binary (install.sh --install-dir).
	InstallDir string
	// Studio is true when the desktop app was installed too (so an update must not pass --cli-only).
	Studio bool
	// AppDir is, on macOS, the directory holding "Vibe Studio.app" (VIBE_APP_DIR).
	AppDir string
	// StudioPath is the Linux vibe-studio binary, or the macOS .app directory.
	StudioPath string

	files []string
	dirs  []string
}

// manifestPath matches install.sh: macOS ~/Library/Application Support/vibe/install-manifest,
// elsewhere ${XDG_DATA_HOME:-~/.local/share}/vibe/install-manifest.
func (u *Updater) manifestPath() (string, error) {
	if u.goos == "darwin" {
		home, err := u.home()
		if err != nil {
			return "", fmt.Errorf("failed to locate the home directory: %w", err)
		}
		return filepath.Join(home, "Library", "Application Support", "vibe", "install-manifest"), nil
	}
	if d := u.getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "vibe", "install-manifest"), nil
	}
	home, err := u.home()
	if err != nil {
		return "", fmt.Errorf("failed to locate the home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "vibe", "install-manifest"), nil
}

// ReadManifest parses an install.sh manifest: "version=X", "F <sha256> <path>" and "D - <path>".
// Unknown lines are ignored.
func ReadManifest(path string) (*Install, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	in := &Install{ManifestPath: path}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "version="):
			if in.Version == "" {
				in.Version = strings.TrimPrefix(strings.TrimPrefix(line, "version="), "v")
			}
		case strings.HasPrefix(line, "F "), strings.HasPrefix(line, "D "):
			// "<kind> <hash or -> <path>": the path may contain spaces.
			parts := strings.SplitN(line, " ", 3)
			if len(parts) == 3 && filepath.IsAbs(parts[2]) {
				if parts[0] == "F" {
					in.files = append(in.files, parts[2])
				} else {
					in.dirs = append(in.dirs, parts[2])
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}
	in.derive()
	return in, nil
}

// derive fills InstallDir, Studio and AppDir from the recorded entries.
func (in *Install) derive() {
	for _, p := range in.files {
		switch filepath.Base(p) {
		case "vibe":
			if in.InstallDir == "" {
				in.InstallDir = filepath.Dir(p)
			}
		case "vibe-studio":
			in.Studio, in.StudioPath = true, p
			if in.InstallDir == "" {
				in.InstallDir = filepath.Dir(p)
			}
		}
	}
	for _, d := range in.dirs {
		if filepath.Base(d) == "Vibe Studio.app" {
			in.Studio, in.StudioPath, in.AppDir = true, d, filepath.Dir(d)
		}
	}
}

// owns reports whether exe (symlinks resolved) is one of the recorded files, or
// lies inside a recorded directory (the macOS app bundle).
func (in *Install) owns(exe string) bool {
	for _, p := range in.files {
		if r, err := filepath.EvalSymlinks(p); err == nil && r == exe {
			return true
		}
	}
	for _, d := range in.dirs {
		if r, err := filepath.EvalSymlinks(d); err == nil && strings.HasPrefix(exe, r+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// Detect returns the installer's record when the running executable is one of
// the files it installed, and an error wrapping ErrExternal otherwise.
func (u *Updater) Detect() (*Install, error) {
	mp, err := u.manifestPath()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrExternal, err)
	}
	in, err := ReadManifest(mp)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w (no install manifest)", ErrExternal)
	}
	if err != nil {
		return nil, fmt.Errorf("%w (%v)", ErrExternal, err)
	}
	exe, err := u.exe()
	if err != nil {
		return nil, fmt.Errorf("%w (cannot locate the running executable)", ErrExternal)
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	if in.InstallDir == "" || !in.owns(exe) {
		return nil, fmt.Errorf("%w (the running program is not in the install manifest)", ErrExternal)
	}
	return in, nil
}
