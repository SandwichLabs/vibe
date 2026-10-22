package main

import (
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
	"time"
)

// gateProbe runs inside the page gate's view (the test binary, mounted where the
// helper is) and prints what a process there can reach. It is the P4 check that
// no known resolver, bus or session socket is connectable, and that the view
// holds only what the allow-list names. One line per fact, "key: value".
func gateProbe(args []string) int {
	uid := os.Getuid()
	runDir := os.Getenv("XDG_RUNTIME_DIR")
	paths := []string{
		"/run/systemd/resolve/io.systemd.Resolve", "/run/systemd/resolve/io.systemd.Resolve.Monitor",
		"/run/avahi-daemon/socket", "/run/dbus/system_bus_socket", "/var/run/dbus/system_bus_socket",
		"/run/nscd/socket", "/var/run/nscd/socket", "/var/lib/sss/pipes/nss", "/run/systemd/private",
		"/run/systemd/journal/stdout", "/run/systemd/journal/socket", "/run/docker.sock", "/var/run/docker.sock",
		"/run/podman/podman.sock", "/tmp/.X11-unix/X0", "@/tmp/.X11-unix/X0",
		fmt.Sprintf("/run/user/%d/bus", uid), fmt.Sprintf("/run/user/%d/systemd/private", uid),
		fmt.Sprintf("/run/user/%d/pipewire-0", uid), fmt.Sprintf("/run/user/%d/pulse/native", uid),
		fmt.Sprintf("/run/user/%d/gcr/ssh", uid), fmt.Sprintf("/run/user/%d/keyring/ssh", uid),
	}
	if runDir != "" {
		paths = append(paths, runDir+"/bus", runDir+"/wayland-0")
	}
	for _, p := range paths {
		c, err := net.DialTimeout("unix", p, time.Second)
		if err == nil {
			_ = c.Close()
			fmt.Printf("unix %s: CONNECTED\n", p)
			continue
		}
		fmt.Printf("unix %s: %s\n", p, shortErr(err))
	}
	if len(args) > 0 {
		c, err := net.DialTimeout("tcp", args[0], time.Second)
		if err == nil {
			_ = c.Close()
			fmt.Printf("tcp %s: CONNECTED\n", args[0])
		} else {
			fmt.Printf("tcp %s: %s\n", args[0], shortErr(err))
		}
	}
	_, err := net.LookupHost("example.com")
	fmt.Printf("dns example.com: %v\n", err != nil)
	_, err = net.LookupHost("localhost")
	fmt.Printf("hosts localhost: %v\n", err == nil)
	if b, err := os.ReadFile("/etc/resolv.conf"); err == nil {
		fmt.Printf("resolv.conf bytes: %d\n", len(b))
	}
	if b, err := os.ReadFile("/etc/nsswitch.conf"); err == nil {
		fmt.Printf("nsswitch: %q\n", string(b))
	}
	ifs, _ := net.Interfaces()
	var names []string
	for _, i := range ifs {
		names = append(names, i.Name)
	}
	fmt.Printf("interfaces: %s\n", strings.Join(names, ","))
	for _, d := range []string{"/", "/run", runDir, "/tmp", "/etc", "/var", "/home", "/root", "/mnt", "/media", "/srv", "/opt"} {
		fmt.Printf("ls %s: %s\n", d, list(d))
	}
	fmt.Printf("env: %s\n", strings.Join(sortedEnv(), " "))
	return 0
}

func shortErr(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 {
		s = s[i+2:]
	}
	return s
}

func list(dir string) string {
	if dir == "" {
		return "(unset)"
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return "ERR " + shortErr(err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

func sortedEnv() []string {
	var out []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
