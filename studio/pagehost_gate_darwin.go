//go:build darwin

package main

// The macOS gate (docs/plans/html-artifacts.md, H4). WebKit's Networking process
// is started by launchd, not by the helper, so no namespace applies and there is
// no sandbox-exec (it is deprecated and does not ship). What gates the network
// here is inside the helper's web view: the strict CSP, the navigation policy, a
// WKContentRuleList that blocks every URL but vibe-page:, and a dead-loopback
// proxy (pagewindow_darwin.m). One leak is accepted and disclosed: a link to an
// IP address that page script clicks opens an empty TCP connection (#189).
//
// What this file does is the launch: a scrubbed (empty) environment, only the two
// pipes, its own process group, and a refusal below macOS 14, where the proxy
// setting does not exist.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// errPageNeedsNewerMac is why a Mac below 14 gets no page window.
var errPageNeedsNewerMac = errors.New("page windows need macOS 14 or later")

// gatedCommand builds the helper's command line. The environment is empty: the
// P1 run measured that WKWebView needs none of it.
func gatedCommand(exe string, in, out *os.File, _ func(string) string) (*exec.Cmd, func(), error) {
	if err := pageHostPreflight(); err != nil {
		return nil, nil, err
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		// The binary on disk is gone or replaced (an update): the helper must be this
		// version, so there is no helper until Studio is restarted.
		return nil, nil, errPageNeedsRestart
	}
	cmd := exec.Command(resolved, "--view-host")
	cmd.Env = []string{}
	cmd.ExtraFiles = []*os.File{in, out}
	// No Pdeathsig on macOS: the helper exits when its pipe closes. A process
	// group takes the helper; the WebKit XPC services are launchd's children and
	// died with the helper in P1 (P4 re-checks).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd, func() {}, nil
}

// macOSMajor is the major version of the running macOS ("14.5.1" is 14).
func macOSMajor() int {
	v, err := syscall.Sysctl("kern.osproductversion")
	if err != nil {
		return 0
	}
	major, _, _ := strings.Cut(v, ".")
	n, err := strconv.Atoi(major)
	if err != nil {
		return 0
	}
	return n
}

// pageHostPreflight refuses macOS 13 and older, where WKWebsiteDataStore has no
// proxyConfigurations, so one of the four layers cannot be set.
func pageHostPreflight() error {
	if !nativeAvailable {
		return errPageWindowUnavailable
	}
	if macOSMajor() < 14 {
		return errPageNeedsNewerMac
	}
	return nil
}
