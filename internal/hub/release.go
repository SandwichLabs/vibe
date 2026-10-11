package hub

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/SandwichLabs/vibe/internal/workspace"
)

// ErrNotReleasable means the holder cannot be asked to let go (it does not
// advertise a release control, or runs on another host).
var ErrNotReleasable = errors.New("the process that has this workspace open cannot be asked to release it")

func requestRelease(root string) error {
	h, err := ReadHolder(root)
	if err != nil {
		return err
	}
	if h.PID == os.Getpid() {
		return errors.New("this process already holds the workspace")
	}
	if host, herr := os.Hostname(); herr != nil || h.Host != host {
		return fmt.Errorf("%w: %s is on another host", ErrNotReleasable, h)
	}
	if h.Control != ControlSigusr1 {
		return fmt.Errorf("%w: %s does not support it", ErrNotReleasable, h)
	}
	pid := h.PID
	return sendRelease(pid, func() error {
		// Re-verify with the process pinned: the lock is still held and still
		// names this pid.
		again, err := ReadHolder(root)
		if err != nil {
			return err
		}
		if again.PID != pid || again.Control != ControlSigusr1 {
			return fmt.Errorf("%w: the holder changed", ErrNotReleasable)
		}
		return nil
	})
}

// TakeOver asks the owner to release the hub and acquires it as app,
// waiting up to timeout (default 10s). It returns the new Hold; on timeout the
// owner is left running and the error says who still holds it. If nobody holds
// the hub it simply acquires it.
func TakeOver(ctx context.Context, ws workspace.Workspace, app string, timeout time.Duration, opts ...Option) (*Hold, error) {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	root := ws.Root()
	if hold, err := acquireAt(root, app, opts...); err == nil {
		return hold, nil
	} else if !errors.Is(err, ErrHubHeld) {
		return nil, err
	}
	if err := requestRelease(root); err != nil && !errors.Is(err, ErrNotHeld) {
		return nil, fmt.Errorf("failed to ask the current owner to release the workspace: %w", err)
	}
	signalled := 0
	if h, err := ReadHolder(root); err == nil {
		signalled = h.PID
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			h, _ := ReadHolder(root)
			who := "it"
			if h != nil {
				who = h.String()
			}
			return nil, fmt.Errorf("%w: %s did not let go of the workspace in time", ErrHubHeld, who)
		case <-tick.C:
			hold, err := acquireAt(root, app, opts...)
			if err == nil {
				return hold, nil
			}
			if !errors.Is(err, ErrHubHeld) {
				return nil, err
			}
			// A client that auto-reconnects can start a new owner the moment
			// the old one lets go: ask that one too.
			var held *HeldError
			if errors.As(err, &held) && held.Holder != nil && held.Holder.PID != signalled &&
				held.Holder.PID != os.Getpid() && held.Holder.Control == ControlSigusr1 {
				signalled = held.Holder.PID
				_ = requestRelease(root)
			}
		}
	}
}
