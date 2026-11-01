//go:build linux

package main

// The Linux network gate (docs/plans/html-artifacts.md, H4, and Spike P1). A
// scripted page's web view must have no network and no way to resolve a name, so
// the helper only ever runs inside bubblewrap with:
//
//   - new user, net, pid, ipc, uts and cgroup namespaces, the real uid mapped to
//     itself (mapping root made GTK use HOME=/root and broke WebKit's own
//     sandbox), no capabilities, a new session;
//   - an allow-listed filesystem view: /usr (read-only), a generated
//     nsswitch.conf of `hosts: files`, an empty resolv.conf, a tmpfs /run with
//     only the Wayland socket bound back in. P1 found that a network namespace
//     alone leaves name resolution open: systemd-resolved's varlink socket, avahi
//     and the system bus all answered from inside it. A deny-list of known
//     sockets can never be complete, so the view lists what is there instead;
//   - the process group and the pid namespace as the tree kill.
//
// If any of that cannot be set up, there is no helper: no code path starts it
// without the gate.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"syscall"
	"time"
)

const bwrapPath = "/usr/bin/bwrap"

// helperHome is the helper's HOME inside the namespace: a tmpfs, never the
// person's home and never /root.
const helperHome = "/tmp/assay-home"

// helperExe is where the Studio binary appears inside the namespace.
const helperExe = "/assay-view-host"

var waylandNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// gateEnv is the whole environment the gate passes to bubblewrap: what the
// toolkit needs to find the compositor, and nothing of the person's (no AWS_*,
// no tokens, no passwords, no workspace .env, no HOME, no DISPLAY).
type gateEnv struct {
	runtimeDir string
	wayland    string // the socket's file name in runtimeDir
}

// lookupGateEnv reads the two variables and checks the Wayland socket is there.
func lookupGateEnv(getenv func(string) string) (gateEnv, error) {
	dir := getenv("XDG_RUNTIME_DIR")
	if dir == "" || !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
		return gateEnv{}, errPageNoDisplay
	}
	name := getenv("WAYLAND_DISPLAY")
	if name == "" {
		name = "wayland-0"
	}
	if !waylandNameRe.MatchString(name) {
		return gateEnv{}, errPageNoDisplay
	}
	fi, err := os.Stat(filepath.Join(dir, name))
	if err != nil || fi.Mode()&os.ModeSocket == 0 {
		return gateEnv{}, errPageNoDisplay
	}
	return gateEnv{runtimeDir: dir, wayland: name}, nil
}

// processEnv is bubblewrap's own environment: XDG_RUNTIME_DIR, and
// WAYLAND_DISPLAY only when it is not the default.
func (g gateEnv) processEnv() []string {
	env := []string{"XDG_RUNTIME_DIR=" + g.runtimeDir}
	if g.wayland != "wayland-0" {
		env = append(env, "WAYLAND_DISPLAY="+g.wayland)
	}
	return env
}

// gateContents are the generated files bound over /etc inside the namespace.
func gateContents(uid, gid int) map[string]string {
	return map[string]string{
		// Names resolve from files only: there is nothing to ask.
		"nsswitch.conf": "passwd: files\ngroup: files\nhosts: files\n",
		"resolv.conf":   "",
		"hosts":         "127.0.0.1 localhost\n::1 localhost\n",
		"passwd":        fmt.Sprintf("assay:x:%d:%d:Assay page:%s:/usr/sbin/nologin\n", uid, gid, helperHome),
		"group":         fmt.Sprintf("assay:x:%d:\n", gid),
	}
}

// gateFiles hands the generated files to bubblewrap as descriptors (--ro-bind-data),
// so no file or directory is ever made on the host: each is the read end of a pipe
// already holding its contents. files go in cmd.ExtraFiles starting at descriptor
// firstFD, and fds names each one's number inside bubblewrap.
type gateFiles struct {
	files []*os.File
	fds   map[string]int
}

func writeGateFiles(uid, gid, firstFD int) (gateFiles, error) {
	contents := gateContents(uid, gid)
	names := make([]string, 0, len(contents))
	for n := range contents {
		names = append(names, n)
	}
	sort.Strings(names)
	g := gateFiles{fds: map[string]int{}}
	for i, n := range names {
		r, w, err := os.Pipe()
		if err != nil {
			g.remove()
			return gateFiles{}, fmt.Errorf("failed to make the page gate files: %w", err)
		}
		_, err = w.WriteString(contents[n])
		_ = w.Close()
		if err != nil {
			_ = r.Close()
			g.remove()
			return gateFiles{}, fmt.Errorf("failed to write the page gate files: %w", err)
		}
		g.files = append(g.files, r)
		g.fds[n] = firstFD + i
	}
	return g, nil
}

// remove closes Studio's copies of the read ends.
func (f gateFiles) remove() {
	for _, r := range f.files {
		_ = r.Close()
	}
}

// etcBinds are the read-only parts of the host's /etc the toolkit needs: the
// dynamic linker cache, fonts, the time zone and graphics vendor files. Every
// one is optional; /etc/passwd, /etc/hosts, /etc/resolv.conf and
// /etc/nsswitch.conf are the generated ones.
var etcBinds = []string{
	"/etc/ld.so.cache", "/etc/fonts", "/etc/localtime", "/etc/gtk-3.0", "/etc/xdg", "/etc/glvnd", "/etc/os-release",
}

// rootLinks are the usr-merge links to recreate when the host has them.
var rootLinks = []string{"/bin", "/sbin", "/lib", "/lib64", "/lib32", "/libx32"}

// bwrapArgs is the whole bubblewrap command line for the helper. It is a pure
// function of its inputs so a test can assert what the view holds and what it
// does not.
func bwrapArgs(env gateEnv, files gateFiles, exe string, readlink func(string) (string, error)) []string {
	a := []string{
		"--unshare-all", "--die-with-parent", "--new-session", "--cap-drop", "ALL", "--clearenv",
		"--setenv", "XDG_RUNTIME_DIR", env.runtimeDir,
		"--setenv", "HOME", helperHome,
		"--setenv", "PATH", "/usr/bin:/bin",
		"--setenv", "NO_AT_BRIDGE", "1",
		"--setenv", "GSETTINGS_BACKEND", "memory",
		"--setenv", "GIO_USE_VFS", "local",
		"--setenv", "GDK_BACKEND", "wayland",
	}
	if env.wayland != "wayland-0" {
		a = append(a, "--setenv", "WAYLAND_DISPLAY", env.wayland)
	}
	a = append(a, "--ro-bind", "/usr", "/usr")
	for _, l := range rootLinks {
		if target, err := readlink(l); err == nil {
			a = append(a, "--symlink", target, l)
		}
	}
	for _, p := range etcBinds {
		a = append(a, "--ro-bind-try", p, p)
	}
	for _, name := range []string{"nsswitch.conf", "resolv.conf", "hosts", "passwd", "group"} {
		a = append(a, "--ro-bind-data", strconv.Itoa(files.fds[name]), "/etc/"+name)
	}
	a = append(a,
		"--ro-bind-try", "/var/cache/fontconfig", "/var/cache/fontconfig",
		"--proc", "/proc", "--dev", "/dev", "--dev-bind-try", "/dev/dri", "/dev/dri",
		"--ro-bind", "/sys", "/sys",
		"--tmpfs", "/tmp", "--tmpfs", "/run",
		"--perms", "0700", "--dir", env.runtimeDir,
		"--bind", filepath.Join(env.runtimeDir, env.wayland), filepath.Join(env.runtimeDir, env.wayland),
		"--perms", "0700", "--dir", helperHome,
		"--ro-bind", exe, helperExe,
		"--chdir", "/",
		"--", helperExe, "--view-host",
	)
	return a
}

// gatedCommand is the one way the helper is built on Linux. in and out are the
// helper's ends of the two pipes (descriptors 3 and 4); the returned cleanup
// removes the generated files once the helper has exited. It refuses, before
// anything is built, on a system that cannot make the gate.
func gatedCommand(exe string, in, out *os.File, getenv func(string) string) (*exec.Cmd, func(), error) {
	if err := linuxGatePreflight(getenv); err != nil {
		return nil, nil, err
	}
	return buildGatedCommand(exe, in, out, getenv)
}

// buildGatedCommand builds the bubblewrap command without probing the system.
func buildGatedCommand(exe string, in, out *os.File, getenv func(string) string) (*exec.Cmd, func(), error) {
	env, err := lookupGateEnv(getenv)
	if err != nil {
		return nil, nil, err
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		// The binary on disk is gone or replaced (an update, or a dev rebuild): the
		// helper must be this version, so there is no helper until Studio is restarted.
		return nil, nil, errPageNeedsRestart
	}
	files, err := writeGateFiles(os.Getuid(), os.Getgid(), 3+2)
	if err != nil {
		return nil, nil, errPageWindowSetup
	}
	cmd := exec.Command(bwrapPath, bwrapArgs(env, files, resolved, os.Readlink)...)
	cmd.Env = env.processEnv()
	cmd.ExtraFiles = append([]*os.File{in, out}, files.files...)
	// Own process group for the tree kill. Pdeathsig is a second chain with the
	// pipe's EOF: either one ends the helper when Studio goes.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	return cmd, files.remove, nil
}

// linuxGatePreflight checks, before anything starts, that the gate can be built
// here: bubblewrap exists, a Wayland session is there, and bubblewrap can make
// the namespaces (Ubuntu's apparmor_restrict_unprivileged_userns, for one,
// stops it). A system that cannot is refused; it is never given a helper with
// less.
func linuxGatePreflight(getenv func(string) string) error {
	env, err := lookupGateEnv(getenv)
	if err != nil {
		return err
	}
	if fi, err := os.Stat(bwrapPath); err != nil || fi.Mode()&0o111 == 0 {
		return errPageNoGate
	}
	files, err := writeGateFiles(os.Getuid(), os.Getgid(), 3)
	if err != nil {
		return errPageWindowSetup
	}
	defer files.remove()
	args := bwrapArgs(env, files, bwrapPath, os.Readlink)
	// Same view and flags, running /usr/bin/true instead of the helper.
	for i, s := range args {
		if s == "--" {
			args = append(args[:i+1:i+1], "/usr/bin/true")
			break
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	probe := exec.CommandContext(ctx, bwrapPath, args...)
	probe.Env = env.processEnv()
	probe.ExtraFiles = files.files
	probe.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := probe.Run(); err != nil {
		return errPageNoGate
	}
	return nil
}

// pageHostPreflight is what Studio checks before it spends anything on a page
// window.
func pageHostPreflight() error {
	if !nativeAvailable {
		return errPageWindowUnavailable
	}
	return linuxGatePreflight(os.Getenv)
}
