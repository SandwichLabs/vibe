//go:build windows

package main

import (
	"context"
	"log/slog"
)

// launcherFor: there is no page window on Windows.
func launcherFor(exeRecord) helperLauncher {
	return func(context.Context, *slog.Logger) (helperConn, error) { return nil, errPageWindowUnavailable }
}
