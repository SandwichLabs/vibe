// Command studio is the Vibe desktop app: a Wails v2 shell whose bound App
// exposes the workspace, connections, notebooks and agent to the Vue frontend.
package main

import (
	"context"
	"embed"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/SandwichLabs/vibe/internal/connections"
	"github.com/SandwichLabs/vibe/internal/pagehost"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

//go:embed all:frontend/dist
var assets embed.FS

// viewWorkerJS is the host-owned bootstrap every custom view's Worker starts from.
//
//go:embed viewhost/worker.js
var viewWorkerJS []byte

// Version info set via ldflags (`task install:studio`).
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Printf("vibe-studio %s (commit: %s, built: %s)\n", version, commit, date)
		return
	}

	// A page window's helper (docs/plans/html-artifacts.md, H4): the same binary,
	// started by Studio under the platform's network gate. It never starts Wails,
	// builds no App and reads no workspace; it talks to Studio over two pipes.
	if len(os.Args) > 1 && os.Args[1] == pagehost.HelperArg {
		os.Exit(runPageHelper())
	}

	// The MotherDuck connection test re-executes the running binary with this
	// hidden argument (one MotherDuck token per process, #115). When Studio is
	// that binary it answers the same way `vibe __conn-test` does, before any
	// window or app state exists.
	if len(os.Args) > 1 && os.Args[1] == connections.ChildCommand {
		if err := connections.RunChild(context.Background(), os.Stdin, os.Stdout); err != nil {
			os.Exit(2)
		}
		return
	}

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	app := NewApp(logger)
	// A workspace folder or .vibe bundle to open at launch (`vibe studio --gui
	// <dir>`; a desktop entry passes file:// URIs), else the workspace around
	// the working directory when started from a terminal (#164).
	app.setLaunchFromEnvironment(os.Args[1:])

	err := wails.Run(&options.App{
		Title:  "Vibe Studio",
		Width:  1280,
		Height: 800,
		AssetServer: &assetserver.Options{
			Assets: assets,
			// Serves /vibe-view/ (custom artifact views and pinned libraries).
			Middleware: func(next http.Handler) http.Handler { return spikeMiddleware(app.views.Middleware()(next)) }, // SPIKE #257 - never merge
		},
		OnStartup:  app.startup,
		OnDomReady: spikeDomReady, // SPIKE #257 - never merge
		OnShutdown: app.shutdown,
		// Lets the frontend flush pending debounced saves (e.g. a notebook's
		// autosave) before the window/process actually closes — see
		// app.beforeClose's doc comment (review fix, step 4a round 2 #1).
		OnBeforeClose: app.beforeClose,
		// macOS delivers "open this with Vibe" (Finder double-click, drop on
		// the Dock icon, `open -a`) as an Apple Event, not argv. It takes the
		// same route as a command-line argument: queued until the frontend is
		// ready, an app:open-path event after.
		Mac: &mac.Options{
			OnFileOpen: app.handleOpenPath,
		},
		Linux: &linux.Options{Icon: appIcon},
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		// app.logger, not logger: only the app's handler redacts (#97).
		app.logger.Error("application error", "error", err)
		os.Exit(1)
	}
}
