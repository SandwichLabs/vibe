//go:build linux

package main

import (
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gateRuntime makes an XDG_RUNTIME_DIR with a Wayland socket in it.
func gateRuntime(t *testing.T, display string) func(string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "xdg")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	l, err := net.Listen("unix", filepath.Join(dir, display))
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	env := map[string]string{
		"XDG_RUNTIME_DIR": dir, "WAYLAND_DISPLAY": display,
		// Everything a person's session has that must not reach the helper.
		"HOME": "/home/zac", "AWS_SECRET_ACCESS_KEY": "s3cr3t", "GITHUB_TOKEN": "t", "DB_PASSWORD": "p",
		"DBUS_SESSION_BUS_ADDRESS": "unix:path=/run/user/1000/bus", "DISPLAY": ":0", "XAUTHORITY": "/home/zac/.Xauthority",
	}
	return func(k string) string { return env[k] }
}

func TestGate_TheHelperIsStartedUnderBubblewrapWithAScrubbedEnvironment(t *testing.T) {
	getenv := gateRuntime(t, "wayland-0")
	in, out := os.NewFile(10, "in"), os.NewFile(11, "out")
	cmd, cleanup, err := gateCommand(getenv, in, out)
	require.NoError(t, err)
	defer cleanup()

	assert.Equal(t, bwrapPath, cmd.Path)
	assert.Equal(t, []string{"XDG_RUNTIME_DIR=" + getenv("XDG_RUNTIME_DIR")}, cmd.Env, "wayland-0 is the default, so only the runtime dir is passed")
	require.GreaterOrEqual(t, len(cmd.ExtraFiles), 2)
	assert.Equal(t, []*os.File{in, out}, cmd.ExtraFiles[:2], "the two pipes first, then only the generated /etc files")
	assert.Len(t, cmd.ExtraFiles, 2+len(gateContents(0, 0)))
	assert.Nil(t, cmd.Stdin)
	assert.Nil(t, cmd.Stdout)
	require.NotNil(t, cmd.SysProcAttr)
	assert.True(t, cmd.SysProcAttr.Setpgid, "its own process group, for the tree kill")
	assert.Equal(t, syscall.SIGKILL, cmd.SysProcAttr.Pdeathsig)

	env := strings.Join(cmd.Env, "\n")
	for _, secret := range []string{"AWS_", "TOKEN", "PASSWORD", "HOME", "DISPLAY=", "XAUTHORITY", "DBUS", "s3cr3t"} {
		assert.NotContains(t, env, secret)
	}
}

func TestGate_ANonDefaultWaylandDisplayIsPassed(t *testing.T) {
	getenv := gateRuntime(t, "wayland-7")
	cmd, cleanup, err := gateCommand(getenv, os.Stdin, os.Stdout)
	require.NoError(t, err)
	defer cleanup()
	assert.ElementsMatch(t, []string{"XDG_RUNTIME_DIR=" + getenv("XDG_RUNTIME_DIR"), "WAYLAND_DISPLAY=wayland-7"}, cmd.Env)
	assert.True(t, slices.Contains(cmd.Args, filepath.Join(getenv("XDG_RUNTIME_DIR"), "wayland-7")), "the socket is bound back by its path")
}

func gateCommand(getenv func(string) string, in, out *os.File) (*exec.Cmd, func(), error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, nil, err
	}
	return buildGatedCommand(exe, in, out, getenv)
}

func argsAfter(args []string, flag string) [][]string {
	var out [][]string
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			out = append(out, args[i+1:min(i+4, len(args))])
		}
	}
	return out
}

func TestGate_TheViewIsAnAllowList(t *testing.T) {
	getenv := gateRuntime(t, "wayland-0")
	cmd, cleanup, err := gateCommand(getenv, os.Stdin, os.Stdout)
	require.NoError(t, err)
	defer cleanup()
	args := cmd.Args[1:]
	joined := strings.Join(args, " ")

	// Namespaces: everything, the network included; no capabilities; a session of its own.
	for _, want := range []string{"--unshare-all", "--die-with-parent", "--new-session", "--cap-drop ALL", "--clearenv"} {
		assert.Contains(t, joined, want)
	}
	assert.NotContains(t, joined, "--share-net")
	assert.NotContains(t, joined, "--dev-bind / /", "no view of the host's root")
	assert.NotContains(t, joined, "--bind / /")
	assert.NotContains(t, joined, "--ro-bind / /")

	// A tmpfs over /run with only the Wayland socket bound back in.
	assert.Contains(t, joined, "--tmpfs /run")
	run := getenv("XDG_RUNTIME_DIR")
	sock := filepath.Join(run, "wayland-0")
	assert.Contains(t, joined, "--bind "+sock+" "+sock)
	for _, sentinel := range []string{"/run/systemd", "/run/dbus", "/var/run", "avahi", "nscd", "resolved", "/run/user/", "sssd", "system_bus", "docker.sock", "/home", "/root", ".ssh"} {
		if sentinel == "/run/user/" && strings.HasPrefix(run, "/run/user/") {
			continue
		}
		assert.NotContains(t, strings.ReplaceAll(joined, run, "RUN"), sentinel, "the view holds no %s", sentinel)
	}

	// Names resolve from files only: the generated nsswitch and an empty resolv.conf,
	// handed over as descriptors (no file is made on the host).
	contents := gateContents(os.Getuid(), os.Getgid())
	assert.Contains(t, contents["nsswitch.conf"], "hosts: files\n")
	assert.NotContains(t, contents["nsswitch.conf"], "dns")
	assert.NotContains(t, contents["nsswitch.conf"], "resolve")
	assert.Empty(t, contents["resolv.conf"])
	bound := map[string]bool{}
	for _, b := range argsAfter(args, "--ro-bind-data") {
		bound[b[1]] = true
	}
	for name := range contents {
		assert.True(t, bound["/etc/"+name], name)
	}
	assert.Len(t, cmd.ExtraFiles, 2+len(contents), "the two pipes, then one descriptor per generated file")

	// The home is a tmpfs path, never /root and never the person's.
	assert.Contains(t, joined, "--setenv HOME "+helperHome)
	assert.NotContains(t, joined, "/root")

	// What runs is this executable, mounted read-only, in helper mode.
	assert.Equal(t, []string{helperExe, "--view-host"}, args[len(args)-2:])
	assert.Equal(t, "--", args[len(args)-3])
}

func TestGate_FailsClosedWithoutADisplay(t *testing.T) {
	good := gateRuntime(t, "wayland-0")
	dir := good("XDG_RUNTIME_DIR")
	for name, env := range map[string]map[string]string{
		"no runtime dir":      {},
		"relative dir":        {"XDG_RUNTIME_DIR": "run/user/1"},
		"unclean dir":         {"XDG_RUNTIME_DIR": dir + "/../" + filepath.Base(dir)},
		"no socket":           {"XDG_RUNTIME_DIR": dir, "WAYLAND_DISPLAY": "wayland-9"},
		"a path as a display": {"XDG_RUNTIME_DIR": dir, "WAYLAND_DISPLAY": "/tmp/evil"},
		"a dot dot display":   {"XDG_RUNTIME_DIR": dir, "WAYLAND_DISPLAY": ".."},
		"a regular file":      {"XDG_RUNTIME_DIR": os.TempDir(), "WAYLAND_DISPLAY": "x"},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := buildGatedCommand(os.Args[0], os.Stdin, os.Stdout, func(k string) string { return env[k] })
			assert.ErrorIs(t, err, errPageNoDisplay)
			assert.ErrorIs(t, linuxGatePreflight(func(k string) string { return env[k] }), errPageNoDisplay)
		})
	}
}

func TestGate_NoFileOrDirectoryIsMadeOnTheHost(t *testing.T) {
	before, _ := filepath.Glob(filepath.Join(os.TempDir(), "assay-pagegate-*"))
	getenv := gateRuntime(t, "wayland-0")
	cmd, cleanup, err := gateCommand(getenv, os.Stdin, os.Stdout)
	require.NoError(t, err)
	cleanup()
	after, _ := filepath.Glob(filepath.Join(os.TempDir(), "assay-pagegate-*"))
	assert.Equal(t, len(before), len(after))
	assert.NotContains(t, strings.Join(cmd.Args, " "), "assay-pagegate")
}

func TestExeRecordRefusesAReplacedBinary(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "studio")
	require.NoError(t, os.WriteFile(bin, []byte("v1"), 0o755))
	rec := exeRecordFor(t, bin)
	assert.True(t, rec.unchanged())

	// An update renames a new file over the old one: same path, new inode.
	next := filepath.Join(dir, "studio.new")
	require.NoError(t, os.WriteFile(next, []byte("v2"), 0o755))
	require.NoError(t, os.Rename(next, bin))
	assert.False(t, rec.unchanged(), "the path now names another file")

	require.NoError(t, os.Remove(bin))
	assert.False(t, rec.unchanged(), "a removed binary")
	assert.False(t, exeRecord{}.unchanged(), "no record, no helper")

	launch := launcherFor(rec)
	_, err := launch(t.Context(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	assert.ErrorIs(t, err, errPageNeedsRestart)
}

func exeRecordFor(t *testing.T, path string) exeRecord {
	t.Helper()
	dev, ino, ok := fileID(path)
	require.True(t, ok)
	return exeRecord{path: path, dev: dev, ino: ino, ok: true}
}
