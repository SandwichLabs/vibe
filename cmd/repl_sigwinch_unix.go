//go:build unix

package cmd

import (
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"

	"github.com/SandwichLabs/assay/internal/repl"
)

// watchResize tells the console when the terminal is resized. The returned
// func stops watching.
func watchResize(fd int, s *repl.Session) func() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ch:
				if w, h, err := term.GetSize(fd); err == nil {
					s.Resize(w, h)
				}
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(ch)
		close(done)
	}
}
