//go:build !windows

package main

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
)

// procConn is a running helper process: its two pipes, and the means to end it
// and everything it started.
type procConn struct {
	toHelper   *os.File
	fromHelper *os.File
	cmd        *exec.Cmd
	cleanup    func()
	done       chan struct{}
	killOnce   sync.Once
}

func (p *procConn) Read(b []byte) (int, error)  { return p.fromHelper.Read(b) }
func (p *procConn) Write(b []byte) (int, error) { return p.toHelper.Write(b) }
func (p *procConn) Done() <-chan struct{}       { return p.done }

// Kill ends the helper's whole process group: the helper, and on Linux the
// bubblewrap parents and the web process under them. P1 found that killing the
// helper alone left a spinning web process alive.
func (p *procConn) Kill() {
	p.killOnce.Do(func() {
		if p.cmd.Process != nil {
			_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
			_ = p.cmd.Process.Kill()
		}
		_ = p.toHelper.Close()
	})
}

// commandBuilder makes the helper's command around the helper's two pipe ends.
type commandBuilder func(in, out *os.File) (*exec.Cmd, func(), error)

// launcherFor starts the helper (this same executable in helper mode) through the
// platform's gate. It never starts it any other way: a platform with no gate has no
// launcher. The binary must still be the file Studio started from.
func launcherFor(rec exeRecord) helperLauncher {
	return func(_ context.Context, logger *slog.Logger) (helperConn, error) {
		if !rec.unchanged() {
			logger.Debug("page window: the Studio binary was replaced since it started")
			return nil, errPageNeedsRestart
		}
		return startHelper(logger, func(in, out *os.File) (*exec.Cmd, func(), error) {
			return gatedCommand(rec.path, in, out, os.Getenv)
		})
	}
}

// startHelper makes the two pipes, builds the command around their child ends,
// starts it, and returns the parent's side.
func startHelper(logger *slog.Logger, build commandBuilder) (helperConn, error) {
	toR, toW, err := os.Pipe()
	if err != nil {
		return nil, errPageWindowSetup
	}
	fromR, fromW, err := os.Pipe()
	if err != nil {
		_ = toR.Close()
		_ = toW.Close()
		return nil, errPageWindowSetup
	}
	closeAll := func() {
		for _, f := range []*os.File{toR, toW, fromR, fromW} {
			_ = f.Close()
		}
	}
	cmd, cleanup, err := build(toR, fromW)
	if err != nil {
		logger.Debug("page window: the helper's command could not be built", "error", err)
		closeAll()
		return nil, err
	}
	cmd.Stderr = &debugLog{logger: logger}
	if err := cmd.Start(); err != nil {
		logger.Debug("page window: the helper did not start", "error", err)
		closeAll()
		cleanup()
		return nil, errPageWindowSetup
	}
	// The child holds its ends now; Studio closes its copies so the helper sees
	// EOF when Studio goes, and Studio when the helper does.
	_ = toR.Close()
	_ = fromW.Close()
	p := &procConn{toHelper: toW, fromHelper: fromR, cmd: cmd, cleanup: cleanup, done: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		cleanup()
		close(p.done)
	}()
	return p, nil
}

// debugLog sends the helper's stderr to Studio's debug log, a line at a time,
// capped: it can hold toolkit warnings and is never shown to anyone.
type debugLog struct {
	logger *slog.Logger
	once   sync.Once
	pw     *io.PipeWriter
}

func (d *debugLog) Write(b []byte) (int, error) {
	d.once.Do(func() {
		pr, pw := io.Pipe()
		d.pw = pw
		go func() {
			sc := bufio.NewScanner(pr)
			sc.Buffer(make([]byte, 0, 4096), 64<<10)
			for sc.Scan() && d.logger != nil {
				d.logger.Debug("page window helper", "line", truncateLog(sc.Text()))
			}
			_, _ = io.Copy(io.Discard, pr)
		}()
	})
	return d.pw.Write(b)
}

func truncateLog(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 {
			return ' '
		}
		return r
	}, s)
	if len(s) > 300 {
		return s[:300] + "..."
	}
	return s
}
