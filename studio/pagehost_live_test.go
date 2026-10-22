//go:build linux && webkit2_41

package main

// Live tests of the Linux page window (step P4): the real helper, under the real
// bubblewrap gate, with a real WebKitGTK view on the person's Wayland session.
// They are skipped unless VIBE_PAGEHOST_LIVE=1, because they open windows.
//
//	VIBE_PAGEHOST_LIVE=1 go test -tags webkit2_41,duckdb_arrow ./studio -run TestLive -v
//
// Ground truth is a loopback listener on the host (TCP and UDP) that logs every
// accept and packet, and /proc for what the helper and its children hold. The
// test binary is the helper (TestMain), mounted into the gate at /vibe-view-host.

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/artifacts"
)

func init() {
	// The control run starts the helper without the network namespace, so the
	// helper's own "I am in the gate" check must be off for it (only in the test binary).
	if os.Getenv("VIBE_LIVE_CONTROL") == "1" {
		helperPreflight = func() error { return nil }
	}
}

func needLive(t *testing.T) {
	t.Helper()
	if os.Getenv("VIBE_PAGEHOST_LIVE") != "1" {
		t.Skip("set VIBE_PAGEHOST_LIVE=1 to open real page windows")
	}
}

// hits is the host's loopback listener: every connection and packet is a leak.
type hits struct {
	mu    sync.Mutex
	lines []string
	port  string
}

func (h *hits) add(f string, a ...any) {
	h.mu.Lock()
	h.lines = append(h.lines, fmt.Sprintf(f, a...))
	h.mu.Unlock()
}

func (h *hits) all() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.lines...)
}

func startHits(t *testing.T) *hits {
	t.Helper()
	h := &hits{}
	tl, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	_, h.port, _ = net.SplitHostPort(tl.Addr().String())
	t.Cleanup(func() { _ = tl.Close() })
	go func() {
		for {
			c, err := tl.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				h.add("tcp-accept from %s", c.RemoteAddr())
				_ = c.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
				if line, err := bufio.NewReader(c).ReadString('\n'); err == nil {
					h.add("http %q", strings.TrimSpace(line))
					_, _ = fmt.Fprint(c, "HTTP/1.1 200 OK\r\nAccess-Control-Allow-Origin: *\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok")
				} else {
					h.add("tcp-noreq")
				}
			}()
		}
	}()
	if ul, err := net.ListenPacket("udp", "127.0.0.1:"+h.port); err == nil {
		t.Cleanup(func() { _ = ul.Close() })
		go func() {
			buf := make([]byte, 2048)
			for {
				n, from, err := ul.ReadFrom(buf)
				if err != nil {
					return
				}
				kind := "udp"
				if n >= 20 && binary.BigEndian.Uint32(buf[4:8]) == 0x2112A442 {
					kind = "STUN"
				}
				h.add("%s from %s len=%d", kind, from, n)
			}
		}()
	}
	return h
}

const probeManifest = `version: 1
title: Probe
view: html
parameters:
  - name: group
    type: text
    default: main
  - name: target
    type: text
    default: "127.0.0.1:1"
  - name: msg
    type: text
    max_length: 1000
    default: ""
queries:
  - name: probe
    sql: SELECT $msg AS msg
html:
  window:
    width: 700
    height: 500
`

// probeApp writes the hostile page as an allowed scripted artifact.
func probeApp(t *testing.T) *App {
	t.Helper()
	app := newOpenApp(t)
	dir := filepath.Join(app.ws.Root(), artifacts.Dir, "probe")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, artifacts.HTMLDir, "assets"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, artifacts.ManifestFile), []byte(probeManifest), 0o644))
	for src, dst := range map[string]string{"index.html.tmpl": "index.html.tmpl", "probe.js": "probe.js"} {
		b, err := os.ReadFile(filepath.Join("testdata", "pagehost-probe", src))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, artifacts.HTMLDir, dst), b, 0o644))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, artifacts.HTMLDir, "assets", "x.json"), []byte(`{"ok":true}`), 0o644))
	// The fixtures' own writes reach the watcher late and would end the window.
	if app.watchDone != nil {
		close(app.watchDone)
		app.watchDone = nil
	}
	if app.watcher != nil {
		_ = app.watcher.Close()
		app.watcher = nil
	}
	st, err := app.ArtifactViewState("probe")
	require.NoError(t, err)
	if st.Error != "" {
		a, gerr := app.GetArtifact("probe")
		require.NoError(t, gerr)
		t.Fatalf("the probe page is refused: %s", a.Error)
	}
	require.Equal(t, "scripted", st.PageKind)
	_, err = app.AllowArtifactView("probe", st.Hash)
	require.NoError(t, err)
	return app
}

// probeRun is one window: what the page reported and how it ended.
type probeRun struct {
	msgs   []string
	states []string
}

// runProbe opens the probe page for a group and returns when the window ends or
// after wait. launch decides how the helper starts.
func runProbe(t *testing.T, app *App, group, target string, wait time.Duration, launch helperLauncher, inspect ...func(*probeRun)) *probeRun {
	t.Helper()
	pr := &probeRun{}
	var mu sync.Mutex
	app.emit = func(name string, data ...any) {
		if name == "pagewindow:state" {
			mu.Lock()
			m, _ := data[0].(map[string]any)
			st, _ := m["state"].(string)
			pr.states = append(pr.states, st)
			mu.Unlock()
		}
	}
	app.pages.launch = launch
	app.pages.preflight = func() error { return nil }
	_, err := app.pages.open(pageOpen{
		ws: app.ws, name: "probe", query: "group=" + group + "&target=" + target, base: context.Background(),
		runner: func(*artifacts.Manifest) artifacts.PageRunner {
			return func(_ context.Context, q string, v map[string]any) (*artifacts.PageRows, error) {
				if q != "probe" {
					return nil, fmt.Errorf("no such query")
				}
				mu.Lock()
				pr.msgs = append(pr.msgs, fmt.Sprint(v["msg"]))
				mu.Unlock()
				return &artifacts.PageRows{Columns: []string{"msg"}, Rows: [][]any{{"ok"}}}, nil
			}
		},
		allowed: func(hash string) bool { return app.pageStillAllowed(app.ws, "probe", hash) },
	})
	require.NoError(t, err)
	start := time.Now()
	deadline := start.Add(wait)
	inspected := len(inspect) == 0
	for time.Now().Before(deadline) {
		if !inspected && time.Since(start) > 4*time.Second {
			inspected = true
			inspect[0](pr)
		}
		mu.Lock()
		ended := len(pr.states) > 0 && pr.states[len(pr.states)-1] != pageStateOpen
		mu.Unlock()
		if ended {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	return pr
}

func (p *probeRun) log() string { return strings.Join(p.msgs, "\n") }

func realLaunch(ctx context.Context, logger *slog.Logger) (helperConn, error) {
	return launcherFor(recordExe())(ctx, logger)
}

// descendants lists every process under root, by walking /proc.
func descendants(root int) []int {
	ents, _ := os.ReadDir("/proc")
	parent := map[int]int{}
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			continue
		}
		s := string(b)
		f := strings.Fields(s[strings.LastIndex(s, ")")+2:])
		pp, _ := strconv.Atoi(f[1])
		parent[pid] = pp
	}
	var out []int
	for pid := range parent {
		for p := parent[pid]; p > 1; p = parent[p] {
			if p == root {
				out = append(out, pid)
				break
			}
		}
	}
	sort.Ints(out)
	return out
}

func cmdlineOf(pid int) string {
	b, _ := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	return strings.ReplaceAll(strings.TrimRight(string(b), "\x00"), "\x00", " ")
}

func TestLive_TheHostilePageReachesNothing(t *testing.T) {
	needLive(t)
	h := startHits(t)
	app := probeApp(t)
	target := "127.0.0.1:" + h.port

	// main, then every navigation and window vector, then the rest.
	logs := map[string]*probeRun{}
	for _, g := range []string{"main", "open", "form", "loc", "alink", "alinkname", "alinkhttps", "metarefresh", "selfnav", "frame", "title", "storage", "charts"} {
		wait := 9 * time.Second
		logs[g] = runProbe(t, app, g, target, wait, realLaunch)
		app.pages.closeAll()
		t.Logf("--- %s: %v\n%s", g, logs[g].states, logs[g].log())
	}
	t.Logf("listener hits: %v", h.all())
	assert.Empty(t, h.all(), "no connection, request or packet reached the host's loopback listener")

	main := logs["main"].log()
	assert.Contains(t, main, `"handler":"object"`)
	assert.Contains(t, main, `"vibe":"object"`)
	assert.Contains(t, main, `"use":"null"`)
	assert.Contains(t, main, "fetch=rejected")
	assert.Contains(t, main, "xhr=rejected")
	assert.Contains(t, main, "fetch-self-asset=ok 200")
	assert.NotContains(t, main, "ws=open")
	assert.Contains(t, main, "run-unknown=That query could not run.")
	assert.Contains(t, main, "getUserMedia=rejected")
	assert.NotContains(t, main, "clipboard=ok")
	assert.NotContains(t, main, "fullscreen=granted")

	for _, g := range []string{"form", "loc", "alink", "alinkname", "alinkhttps", "metarefresh"} {
		assert.Contains(t, logs[g].log(), "still here", "%s: the page was not navigated away", g)
	}
	assert.Contains(t, logs["open"].log(), "window.open=null")
	assert.Contains(t, logs["open"].log(), "open=done")
	sn := logs["selfnav"].log()
	assert.NotContains(t, sn, "js=1", "a javascript: link ran nothing")
	for _, href := range []string{"other?x=1", "about:blank", "javascript:window.__js=1", "blob:vibe-page://probe/x", "data:text/html,<p>x", "file:///etc/passwd"} {
		assert.Contains(t, sn, "nav "+href[:min(len(href), 20)]+"=still here", "a link to %s went nowhere", href)
	}
	assert.Contains(t, sn, "samepage=arrived", "a same-page link with a query string loads")
	assert.NotContains(t, logs["frame"].log(), "posted-from-srcdoc", "a frame cannot post to the handler")
	assert.NotContains(t, logs["storage"].log(), `"prevLocalStorage":"v1"`)
	assert.Contains(t, logs["storage"].log(), `"prevCookie":""`)
	assert.Contains(t, logs["charts"].log(), "d3=path length")
	assert.Contains(t, logs["charts"].log(), "vega-lite=svg bytes")
	assert.NotContains(t, logs["charts"].log(), "FAIL")
}

// procInfo is what /proc says about one process of the helper's tree.
type procInfo struct {
	pid     int
	cmd     string
	environ []string
	fds     map[string]string
	status  map[string]string
	netNS   string
	netDev  []string
}

func readProc(pid int) procInfo {
	p := procInfo{pid: pid, cmd: cmdlineOf(pid), fds: map[string]string{}, status: map[string]string{}}
	root := fmt.Sprintf("/proc/%d", pid)
	if b, err := os.ReadFile(root + "/environ"); err == nil {
		for _, kv := range strings.Split(strings.TrimRight(string(b), "\x00"), "\x00") {
			if kv != "" {
				p.environ = append(p.environ, kv)
			}
		}
	}
	if ents, err := os.ReadDir(root + "/fd"); err == nil {
		for _, e := range ents {
			p.fds[e.Name()], _ = os.Readlink(root + "/fd/" + e.Name())
		}
	}
	if b, err := os.ReadFile(root + "/status"); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if k, v, ok := strings.Cut(l, ":"); ok {
				p.status[k] = strings.TrimSpace(v)
			}
		}
	}
	p.netNS, _ = os.Readlink(root + "/ns/net")
	if b, err := os.ReadFile(root + "/net/dev"); err == nil {
		for _, l := range strings.Split(string(b), "\n")[2:] {
			if name, _, ok := strings.Cut(strings.TrimSpace(l), ":"); ok {
				p.netDev = append(p.netDev, name)
			}
		}
	}
	return p
}

func envKeys(env []string) []string {
	var keys []string
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func TestLive_TheHelperProcessTree(t *testing.T) {
	needLive(t)
	// Things a person's session has, which must not reach the helper.
	t.Setenv("AWS_SECRET_ACCESS_KEY", "live-test-secret")
	t.Setenv("GITHUB_TOKEN", "live-test-token")
	t.Setenv("DB_PASSWORD", "live-test-password")
	h := startHits(t)
	app := probeApp(t)
	// A descriptor the parent forgot to mark close-on-exec, well above the range a
	// fixed loop would have covered.
	planted, err := os.CreateTemp("", "planted-fd-")
	require.NoError(t, err)
	defer os.Remove(planted.Name())
	require.NoError(t, syscall.Dup3(int(planted.Fd()), 100, 0))
	defer syscall.Close(100)
	var root int
	launch := func(ctx context.Context, l *slog.Logger) (helperConn, error) {
		c, err := launcherFor(recordExe())(ctx, l)
		if err == nil {
			root = pidOf(c)
		}
		return c, err
	}
	var tree []procInfo
	var rootInfo procInfo
	hostNet, _ := os.Readlink("/proc/self/ns/net")
	runProbe(t, app, "storage", "127.0.0.1:"+h.port, 9*time.Second, launch, func(*probeRun) {
		rootInfo = readProc(root)
		for _, pid := range descendants(root) {
			tree = append(tree, readProc(pid))
		}
	})
	app.pages.closeAll()
	require.NotEmpty(t, tree)
	for _, p := range tree {
		t.Logf("pid %d %s net=%v nonewprivs=%s seccomp=%s capeff=%s", p.pid, truncateLog(p.cmd), p.netDev, p.status["NoNewPrivs"], p.status["Seccomp"], p.status["CapEff"])
	}

	// bubblewrap itself is the process Studio started, with the scrubbed environment.
	assert.Equal(t, []string{"XDG_RUNTIME_DIR"}, envKeys(rootInfo.environ), "bubblewrap gets the runtime dir and nothing else")

	var helper procInfo
	for _, p := range tree {
		if strings.Contains(p.cmd, helperExe+" --view-host") {
			helper = p
		}
	}
	require.NotZero(t, helper.pid, "the helper is in the tree")
	assert.Equal(t, []string{"GDK_BACKEND", "GIO_USE_VFS", "GSETTINGS_BACKEND", "HOME", "NO_AT_BRIDGE", "PATH", "PWD", "XDG_RUNTIME_DIR"}, envKeys(helper.environ))
	assert.Contains(t, helper.environ, "HOME="+helperHome)
	for _, kv := range helper.environ {
		assert.NotContains(t, kv, "live-test", "no secret reached the helper")
	}

	// The two pipes are held by the helper only.
	var pipes []string
	for _, fd := range []string{"3", "4"} {
		require.Contains(t, helper.fds, fd)
		assert.True(t, strings.HasPrefix(helper.fds[fd], "pipe:["), "fd %s is %s", fd, helper.fds[fd])
		pipes = append(pipes, helper.fds[fd])
	}
	for _, p := range tree {
		if p.pid == helper.pid {
			continue
		}
		for fd, target := range p.fds {
			assert.NotContains(t, pipes, target, "pid %d (%s) holds the protocol pipe on fd %s", p.pid, truncateLog(p.cmd), fd)
		}
	}
	// Nothing WebKit started holds the planted descriptor: only the gate's own
	// processes and the helper (which marked it close-on-exec) have it.
	for _, p := range tree {
		holds := false
		for _, target := range p.fds {
			if strings.Contains(target, filepath.Base(planted.Name())) {
				holds = true
			}
		}
		own := strings.HasPrefix(p.cmd, "/usr/bin/bwrap --unshare-all --die-with-parent --new-session") || strings.HasPrefix(p.cmd, helperExe)
		if holds && !own {
			t.Errorf("pid %d (%s) holds a descriptor the parent planted", p.pid, truncateLog(p.cmd))
		}
	}
	for fd, target := range helper.fds {
		assert.NotContains(t, target, app.ws.Root(), "fd %s", fd)
		assert.NotContains(t, target, os.Getenv("HOME")+"/", "fd %s", fd)
	}

	// No network: the tree's net namespace is not the host's and holds only lo.
	for _, p := range tree {
		if p.netNS == "" || p.netNS == hostNet {
			t.Errorf("pid %d (%s) shares the host's network namespace", p.pid, truncateLog(p.cmd))
		}
		assert.Equal(t, []string{"lo"}, p.netDev, "pid %d (%s)", p.pid, truncateLog(p.cmd))
	}

	// The web process runs in WebKit's own sandbox, inside the gate.
	var web *procInfo
	for i, p := range tree {
		if strings.Contains(p.cmd, "WebKitWebProcess") {
			web = &tree[i]
		}
	}
	require.NotNil(t, web, "there is a web process")
	assert.Equal(t, "1", web.status["NoNewPrivs"])
	assert.Equal(t, "2", web.status["Seccomp"])
	assert.True(t, strings.HasPrefix(web.status["CapEff"], "0000000000000000"), "no capabilities: %s", web.status["CapEff"])
	assert.Empty(t, h.all())
}

func TestLive_AFrozenPageIsKilledWithItsWholeTree(t *testing.T) {
	needLive(t)
	h := startHits(t)
	app := probeApp(t)
	var root int
	launch := func(ctx context.Context, l *slog.Logger) (helperConn, error) {
		c, err := launcherFor(recordExe())(ctx, l)
		if err == nil {
			root = pidOf(c)
		}
		return c, err
	}
	var before []int
	started := time.Now()
	pr := runProbe(t, app, "freeze", "127.0.0.1:"+h.port, 25*time.Second, launch, func(*probeRun) {
		before = append([]int{root}, descendants(root)...)
	})
	took := time.Since(started)
	t.Logf("states %v after %v; tree before the kill: %d processes", pr.states, took, len(before))
	assert.Equal(t, pageStateStopped, pr.states[len(pr.states)-1])
	require.Greater(t, len(before), 3, "bubblewrap, the helper, the network process and a web process at least")
	time.Sleep(2 * time.Second)
	for _, pid := range before {
		if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); err == nil && !strings.Contains(string(b), ") Z ") {
			t.Errorf("pid %d (%s) outlived the kill", pid, truncateLog(cmdlineOf(pid)))
		}
	}
}

func TestLive_TheViewIsAnAllowListNotADenyList(t *testing.T) {
	needLive(t)
	h := startHits(t)
	exe, err := os.Executable()
	require.NoError(t, err)
	getenv := os.Getenv
	env, err := lookupGateEnv(getenv)
	require.NoError(t, err)
	files, err := writeGateFiles(os.Getuid(), os.Getgid(), 3)
	require.NoError(t, err)
	defer files.remove()
	args := bwrapArgs(env, files, exe, os.Readlink)
	args = append(args[:len(args)-2], helperExe, "--gate-probe", "127.0.0.1:"+h.port)
	cmd := exec.Command(bwrapPath, args...)
	cmd.Env = env.processEnv()
	cmd.ExtraFiles = files.files
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	t.Logf("\n%s", out)
	text := string(out)
	connected := 0
	for _, l := range strings.Split(text, "\n") {
		if strings.Contains(l, "CONNECTED") {
			connected++
			assert.Equal(t, "unix "+filepath.Join(env.runtimeDir, "wayland-0")+": CONNECTED", l, "only the Wayland socket connects")
		}
	}
	assert.Equal(t, 1, connected, "the Wayland socket connects, and no other socket or TCP port does")
	assert.Contains(t, text, "dns example.com: true")
	assert.Contains(t, text, "hosts localhost: true")
	assert.Contains(t, text, "resolv.conf bytes: 0")
	assert.Contains(t, text, `nsswitch: "passwd: files\ngroup: files\nhosts: files\n"`)
	assert.Contains(t, text, "interfaces: lo\n")
	assert.Contains(t, text, "ls /run/user/1000: wayland-0", "the Wayland socket is the one thing in the runtime dir")
	assert.Contains(t, text, "ls /run: user\n")
	assert.Contains(t, text, "ls /: bin,dev,etc,lib,lib64,proc,run,sbin,sys,tmp,usr,vibe-view-host\n")
	assert.Empty(t, h.all())
}

// TestLive_ControlWithoutTheNamespaceTheSameVectorsFire proves the silence in
// the main test is the gate's: the same view, with the network namespace left
// out, lets the click and preconnect vectors reach the listener.
func TestLive_ControlWithoutTheNamespaceTheSameVectorsFire(t *testing.T) {
	needLive(t)
	h := startHits(t)
	app := probeApp(t)
	exe, err := os.Executable()
	require.NoError(t, err)
	launch := func(_ context.Context, l *slog.Logger) (helperConn, error) {
		return startHelper(l, func(in, out *os.File) (*exec.Cmd, func(), error) {
			env, err := lookupGateEnv(os.Getenv)
			if err != nil {
				return nil, nil, err
			}
			files, err := writeGateFiles(os.Getuid(), os.Getgid(), 5)
			if err != nil {
				return nil, nil, err
			}
			args := bwrapArgs(env, files, exe, os.Readlink)
			var shared []string
			for _, a := range args {
				if a == "--unshare-all" {
					shared = append(shared, "--unshare-user", "--unshare-ipc", "--unshare-pid", "--unshare-uts", "--unshare-cgroup-try")
					continue
				}
				shared = append(shared, a)
				if a == "--clearenv" {
					// Keep the network, and tell the test binary (it is the helper) not to check for the namespace.
					shared = append(shared, "--setenv", "VIBE_LIVE_CONTROL", "1")
				}
			}
			cmd := exec.Command(bwrapPath, shared...)
			cmd.Env = env.processEnv()
			cmd.ExtraFiles = append([]*os.File{in, out}, files.files...)
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
			return cmd, files.remove, nil
		})
	}
	for _, g := range []string{"main", "alink"} {
		pr := runProbe(t, app, g, "127.0.0.1:"+h.port, 9*time.Second, launch)
		app.pages.closeAll()
		t.Logf("%s: %v", g, pr.states)
	}
	t.Logf("control hits: %v", h.all())
	assert.NotEmpty(t, h.all(), "without the namespace the vectors reach the listener: the silence elsewhere is the gate")
}

// pidOf is the pid of the process Studio started (bubblewrap on Linux).
func pidOf(c helperConn) int {
	p, ok := c.(*procConn)
	if !ok || p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

const capsManifest = `version: 1
title: Caps
view: html
queries:
  - name: report
    sql: SELECT $msg AS msg
parameters:
  - name: msg
    type: text
    max_length: 1000
    default: ""
html:
  window:
    width: 600
    height: 400
  capabilities:
    state: {scope: local}
    download: {}
    ask: {}
`

// TestLive_CapabilitiesInTheRealWindow opens a scripted page that uses state,
// download and ask in the real helper: state persists across a reopen, a write
// from another window is pushed to the page, download without a gesture is
// refused inside the view, ask goes to the (fake) service, and a value over
// 64 KB is refused.
func TestLive_CapabilitiesInTheRealWindow(t *testing.T) {
	needLive(t)
	app := newOpenApp(t)
	dir := filepath.Join(app.ws.Root(), artifacts.Dir, "caps")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, artifacts.HTMLDir), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, artifacts.ManifestFile), []byte(capsManifest), 0o644))
	for _, f := range []string{"index.html.tmpl", "app.js"} {
		b, err := os.ReadFile(filepath.Join("testdata", "pagehost-caps", f))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, artifacts.HTMLDir, f), b, 0o644))
	}
	if app.watchDone != nil {
		close(app.watchDone)
		app.watchDone = nil
	}
	if app.watcher != nil {
		_ = app.watcher.Close()
		app.watcher = nil
	}
	st, err := app.ArtifactViewState("caps")
	require.NoError(t, err)
	require.Empty(t, st.Error)
	_, err = app.AllowArtifactView("caps", st.Hash)
	require.NoError(t, err)

	var mu sync.Mutex
	var msgs []string
	var asked []string
	var saved atomic.Int32
	app.pages.launch = realLaunch
	app.pages.preflight = func() error { return nil }
	open := func() {
		svc := app.pageServices(app.ws, "caps")
		svc.ask = func(_ context.Context, sys, p string) (string, bool, error) {
			mu.Lock()
			asked = append(asked, sys+"/"+p)
			mu.Unlock()
			return "hi there", true, nil
		}
		svc.save = func(context.Context, string, string, string) (string, error) { saved.Add(1); return "", nil }
		_, err := app.pages.open(pageOpen{
			ws: app.ws, name: "caps", base: context.Background(), svc: svc,
			runner: func(*artifacts.Manifest) artifacts.PageRunner {
				return func(_ context.Context, q string, v map[string]any) (*artifacts.PageRows, error) {
					mu.Lock()
					msgs = append(msgs, fmt.Sprint(v["msg"]))
					mu.Unlock()
					return &artifacts.PageRows{Columns: []string{"msg"}, Rows: [][]any{{"ok"}}}, nil
				}
			},
			allowed: func(h string) bool { return app.pageStillAllowed(app.ws, "caps", h) },
		})
		require.NoError(t, err)
	}
	logText := func() string { mu.Lock(); defer mu.Unlock(); return strings.Join(msgs, "\n") }
	waitFor := func(s string) {
		require.Eventually(t, func() bool { return strings.Contains(logText(), s) }, 40*time.Second, 200*time.Millisecond, "%q in:\n%s", s, logText())
	}

	open()
	waitFor("count=1")
	waitFor("ask={\"text\":\"hi there\"}")
	waitFor("big rejected")
	waitFor("ready")
	t.Log("first run:\n" + logText())
	assert.Contains(t, logText(), "caps state=true download=true ask=true db=false")
	assert.Contains(t, logText(), "save rejected: Saving a file needs a click or a key press first.", "the host's own replies must not count as a gesture")
	assert.Contains(t, logText(), "raw save posted")
	time.Sleep(time.Second) // the raw post, straight to the handler, had its chance to reach Studio
	assert.EqualValues(t, 0, saved.Load(), "no dialog opened without a physical input, even for a call posted past the API")
	assert.Equal(t, []string{"be brief/say hi"}, asked)

	// A write from another window of the same artifact is pushed to the page.
	other, err := app.pages.stateStore(app.ws.Root(), "caps", nil)
	require.NoError(t, err)
	require.NoError(t, other.Set("fromOther", json.RawMessage(`{"n":7}`), "another window"))
	waitFor(`"key":"fromOther"`)

	app.pages.closeArtifact("caps", pageStateClosed)
	require.Eventually(t, func() bool { return pageCount(app.pages) == 0 }, 10*time.Second, 50*time.Millisecond)
	mu.Lock()
	msgs = nil
	mu.Unlock()
	open() // the state survived the window
	waitFor("count=2")
	app.pages.closeAll()
	b, err := os.ReadFile(filepath.Join(app.ws.Root(), ".vibe-local", "artifacts", "caps", "state.json"))
	require.NoError(t, err)
	assert.Contains(t, string(b), `"count":2`)
	assert.Contains(t, string(b), `"fromOther"`)
}
