// Command pagehost is the P1 spike for the scripted page host (H4): a parent that
// stands in for Studio and a helper that owns a WebKitGTK web view. Throwaway.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

func main() {
	runtime.LockOSThread()
	if len(os.Args) > 1 && os.Args[1] == "--helper" {
		fs := flag.NewFlagSet("helper", flag.ExitOnError)
		var c helperCfg
		var ttl float64
		fs.StringVar(&c.csp, "csp", "strict", "")
		fs.StringVar(&c.cspVia, "via", "header", "")
		fs.BoolVar(&c.settings, "settings", true, "")
		fs.BoolVar(&c.policy, "policy", true, "")
		fs.BoolVar(&c.sandbox, "sandbox", true, "")
		fs.StringVar(&c.group, "group", "main", "")
		fs.StringVar(&c.target, "target", "", "")
		fs.Float64Var(&ttl, "ttl", 12, "")
		fs.StringVar(&c.libdir, "libdir", "", "")
		fs.StringVar(&c.inns, "inns", "", "")
		fs.StringVar(&c.dns, "dns", "", "")
		fs.StringVar(&c.listen, "listen", "", "")
		fs.BoolVar(&noCloexec, "no-cloexec", false, "")
		fs.Parse(os.Args[2:])
		c.ttl = time.Duration(ttl * float64(time.Second))
		runHelper(c)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "listener" {
		h, _ := newHitLog(os.Args[3], "HOST")
		port, err := startListeners(os.Args[2], h)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		fmt.Println("listening", port)
		select {}
	}
	parent(os.Args[1:])
}

type runCfg struct {
	name, csp, via, group, ns, dns, logdir, libdir, extraEnv, base, killmode string
	settings, policy, sandbox, watchdog                       bool
	ttl                                                       float64
}

func parent(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	var r runCfg
	fs.StringVar(&r.name, "name", "run", "")
	fs.StringVar(&r.csp, "csp", "strict", "strict|permissive|none")
	fs.StringVar(&r.via, "via", "header", "header|meta|both")
	fs.StringVar(&r.group, "group", "main", "")
	fs.StringVar(&r.ns, "ns", "none", "none|isolated|probe")
	fs.StringVar(&r.dns, "dns", "", "udp53|varlink (probe ns only)")
	fs.StringVar(&r.logdir, "logdir", ".", "")
	fs.StringVar(&r.libdir, "libdir", "", "")
	fs.StringVar(&r.extraEnv, "env", "", "comma-separated extra KEY=VAL")
	fs.StringVar(&r.base, "base", "WAYLAND_DISPLAY,XDG_RUNTIME_DIR,GDK_BACKEND=wayland", "comma-separated base env: NAME (copied) or NAME=VAL")
	fs.BoolVar(&r.settings, "settings", true, "")
	fs.BoolVar(&r.policy, "policy", true, "")
	fs.BoolVar(&r.sandbox, "sandbox", true, "")
	fs.StringVar(&r.killmode, "killmode", "pid", "pid|group|pidns")
	fs.BoolVar(&r.watchdog, "watchdog", false, "kill the helper on a missed heartbeat")
	fs.Float64Var(&r.ttl, "ttl", 10, "")
	fs.Parse(args)
	os.MkdirAll(r.logdir, 0o755)
	lp := func(s string) string { return filepath.Join(r.logdir, r.name+"."+s+".log") }
	for _, s := range []string{"listener", "inns", "parent", "helper", "proc"} {
		os.Remove(lp(s))
	}
	plog, _ := os.OpenFile(lp("parent"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	var plmu sync.Mutex
	logf := func(f string, a ...any) {
		plmu.Lock()
		defer plmu.Unlock()
		fmt.Fprintf(plog, "%s %s\n", time.Now().Format("15:04:05.000"), fmt.Sprintf(f, a...))
	}

	target := ""
	if r.ns == "probe" {
		target = "127.0.0.1:39301"
	} else {
		h, _ := newHitLog(lp("listener"), "HOST")
		port, err := startListeners("127.0.0.1:0", h)
		if err != nil {
			fmt.Println("listener:", err)
			os.Exit(1)
		}
		target = "127.0.0.1:" + port
	}
	logf("config %+v target=%s", r, target)

	// the protocol socketpair; the child's end becomes fd 3
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		panic(err)
	}
	pend := os.NewFile(uintptr(fds[0]), "parent-end")
	cend := os.NewFile(uintptr(fds[1]), "child-end")

	exe, _ := os.Executable()
	hargs := []string{"--helper", "-csp=" + r.csp, "-via=" + r.via, "-group=" + r.group, "-target=" + target,
		fmt.Sprintf("-settings=%v", r.settings), fmt.Sprintf("-policy=%v", r.policy), fmt.Sprintf("-sandbox=%v", r.sandbox),
		fmt.Sprintf("-ttl=%v", r.ttl), "-libdir=" + r.libdir}
	if r.ns == "probe" {
		hargs = append(hargs, "-inns=probe", "-dns="+r.dns, "-listen="+lp("inns"))
	}
	var cmd *exec.Cmd
	if r.ns == "bwrap" {
		cmd = exec.Command("/usr/bin/bwrap", append([]string{"--unshare-net", "--die-with-parent", "--dev-bind", "/", "/", "--", exe}, hargs...)...)
	} else {
		cmd = exec.Command(exe, hargs...)
	}
	// scrubbed environment: start empty, add only what GTK/Wayland need
	env := []string{}
	for _, k := range strings.Split(r.base, ",") {
		if k == "" {
			continue
		}
		if strings.Contains(k, "=") {
			env = append(env, k)
		} else if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	if r.extraEnv != "" {
		env = append(env, strings.Split(r.extraEnv, ",")...)
	}
	cmd.Env = env
	hl, _ := os.OpenFile(lp("helper"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	cmd.Stdout, cmd.Stderr = hl, hl
	cmd.ExtraFiles = []*os.File{cend}
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	if r.killmode == "group" {
		cmd.SysProcAttr.Setpgid = true
	}
	if r.ns != "none" && r.ns != "bwrap" {
		fl := uintptr(syscall.CLONE_NEWUSER | syscall.CLONE_NEWNET)
		if r.ns == "probe" {
			fl |= syscall.CLONE_NEWNS
		}
		if r.killmode == "pidns" {
			fl |= syscall.CLONE_NEWPID
		}
		cmd.SysProcAttr.Cloneflags = fl
		inUID, inGID := os.Getuid(), os.Getgid() // production shape: same ids, no capabilities after exec
		if r.ns == "probe" {
			inUID, inGID = 0, 0 // instrumented: root in the namespace (needs lo up, port 53, mounts)
			hd := filepath.Join(os.TempDir(), "spike-home")
			os.MkdirAll(hd, 0o755)
			cmd.Env = append(cmd.Env, "HOME="+hd)
		}
		cmd.SysProcAttr.UidMappings = []syscall.SysProcIDMap{{ContainerID: inUID, HostID: os.Getuid(), Size: 1}}
		cmd.SysProcAttr.GidMappings = []syscall.SysProcIDMap{{ContainerID: inGID, HostID: os.Getgid(), Size: 1}}
		cmd.SysProcAttr.GidMappingsEnableSetgroups = false
	}
	if err := cmd.Start(); err != nil {
		logf("START FAILED: %v", err)
		fmt.Println("start failed:", err)
		os.Exit(1)
	}
	cend.Close()
	logf("helper started pid=%d env=%v", cmd.Process.Pid, env)
	exited := make(chan struct{})
	go func() { cmd.Wait(); close(exited) }()

	var lastHB atomicTime
	var killedByWatchdog bool
	var wmu sync.Mutex
	write := func(s string) { wmu.Lock(); fmt.Fprintln(pend, s); wmu.Unlock() }
	nraw := 0
	go func() {
		sc := bufio.NewScanner(pend)
		sc.Buffer(make([]byte, 1<<20), 1<<24)
		for sc.Scan() {
			l := sc.Text()
			switch {
			case strings.HasPrefix(l, "P "):
				if nraw++; nraw <= 40 && nraw%8 == 1 {
					logf("raw %s", l)
				}
				var m struct {
					T    string          `json:"t"`
					ID   int             `json:"id"`
					Name string          `json:"name"`
					O    json.RawMessage `json:"o"`
				}
				if json.Unmarshal([]byte(l[2:]), &m) != nil {
					logf("page-msg unparsable: %s", l)
					continue
				}
				switch m.T {
				case "hb":
					lastHB.set(time.Now())
				case "run":
					logf("page run name=%s -> replying fake rows", m.Name)
					write(fmt.Sprintf(`R %d {"columns":["region","total"],"rows":[["east",1],["west",2]],"truncated":false}`, m.ID))
				case "log":
					logf("PAGE %s", m.O)
				default:
					logf("page-msg unknown: %s", l)
				}
			default:
				logf("%s", l)
			}
		}
		logf("protocol pipe closed")
	}()

	start := time.Now()
	snapDone := false
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
loop:
	for {
		select {
		case <-exited:
			break loop
		case now := <-tick.C:
			if !snapDone && now.Sub(start) > time.Duration((r.ttl-2.5)*float64(time.Second)) {
				snapDone = true
				snapshot(cmd.Process.Pid, lp("proc"), logf)
			}
			if r.watchdog && !lastHB.get().IsZero() && now.Sub(lastHB.get()) > 2*time.Second && !killedByWatchdog {
				killedByWatchdog = true
				logf("WATCHDOG: heartbeat missed for %v: killing helper pid %d", now.Sub(lastHB.get()).Round(time.Millisecond), cmd.Process.Pid)
				descendants := descendantsOf(cmd.Process.Pid)
				logf("descendants before kill: %v", descendants)
				if r.killmode == "group" {
					syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				}
				cmd.Process.Kill()
				<-exited
				time.Sleep(1500 * time.Millisecond)
				for _, d := range descendants {
					if _, err := os.Stat("/proc/" + strconv.Itoa(d)); err == nil {
						logf("ORPHAN still alive after helper kill: pid %d %s", d, cmdline(d))
					} else {
						logf("descendant pid %d exited", d)
					}
				}
				break loop
			}
			if now.Sub(start) > time.Duration((r.ttl+4)*float64(time.Second)) {
				logf("hard timeout: killing helper")
				cmd.Process.Kill()
				<-exited
				break loop
			}
		}
	}
	logf("helper exited: %v", cmd.ProcessState)
	time.Sleep(300 * time.Millisecond)
	fmt.Println("done", r.name)
}

type atomicTime struct {
	mu sync.Mutex
	t  time.Time
}

func (a *atomicTime) set(t time.Time) { a.mu.Lock(); a.t = t; a.mu.Unlock() }
func (a *atomicTime) get() time.Time  { a.mu.Lock(); defer a.mu.Unlock(); return a.t }

func ppidOf(pid int) int {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return -1
	}
	s := string(b)
	i := strings.LastIndex(s, ")")
	f := strings.Fields(s[i+2:])
	p, _ := strconv.Atoi(f[1])
	return p
}

func descendantsOf(root int) []int {
	ents, _ := os.ReadDir("/proc")
	parent := map[int]int{}
	for _, e := range ents {
		if pid, err := strconv.Atoi(e.Name()); err == nil {
			parent[pid] = ppidOf(pid)
		}
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

func cmdline(pid int) string {
	b, _ := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	s := strings.ReplaceAll(string(b), "\x00", " ")
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

func snapshot(root int, path string, logf func(string, ...any)) {
	f, _ := os.Create(path)
	defer f.Close()
	pids := append([]int{root}, descendantsOf(root)...)
	hostNS := map[string]string{}
	for _, ns := range []string{"net", "user", "pid", "mnt", "ipc", "uts"} {
		hostNS[ns], _ = os.Readlink("/proc/self/ns/" + ns)
	}
	fmt.Fprintf(f, "# parent namespaces: %v\n", hostNS)
	for _, pid := range pids {
		p := "/proc/" + strconv.Itoa(pid)
		fmt.Fprintf(f, "\n=== pid %d ppid %d: %s\n", pid, ppidOf(pid), cmdline(pid))
		for _, ns := range []string{"net", "user", "pid", "mnt", "ipc"} {
			l, _ := os.Readlink(p + "/ns/" + ns)
			mark := ""
			if l == hostNS[ns] {
				mark = " (same as parent)"
			}
			fmt.Fprintf(f, "ns/%s -> %s%s\n", ns, l, mark)
		}
		st, _ := os.ReadFile(p + "/status")
		for _, line := range strings.Split(string(st), "\n") {
			for _, k := range []string{"Name:", "Uid:", "Gid:", "NSpid:", "NoNewPrivs:", "Seccomp:", "CapEff:", "CapBnd:"} {
				if strings.HasPrefix(line, k) {
					fmt.Fprintln(f, line)
				}
			}
		}
		env, err := os.ReadFile(p + "/environ")
		if err != nil {
			fmt.Fprintf(f, "environ: (%v)\n", err)
		} else {
			fmt.Fprintf(f, "environ:\n  %s\n", strings.ReplaceAll(strings.TrimRight(string(env), "\x00"), "\x00", "\n  "))
		}
		ents, err := os.ReadDir(p + "/fd")
		if err != nil {
			fmt.Fprintf(f, "fd: (%v)\n", err)
		} else {
			fmt.Fprintf(f, "fd:\n")
			for _, e := range ents {
				l, _ := os.Readlink(p + "/fd/" + e.Name())
				fmt.Fprintf(f, "  %s -> %s\n", e.Name(), l)
			}
		}
	}
	logf("snapshot written for pids %v", pids)
}
