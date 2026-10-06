package main

import (
	"context"
	"embed"
	"log/slog"
	"os"

	"github.com/sandwich-labs/vibe-app/internal/connections"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
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
	if len(os.Args) > 1 {
		// A workspace folder to open at launch (`vibe studio --gui <dir>`).
		app.setInitialArg(os.Args[1])
	}

	err := wails.Run(&options.App{
		Title:  "Vibe Studio",
		Width:  1280,
		Height: 800,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		// Lets the frontend flush pending debounced saves (e.g. a notebook's
		// autosave) before the window/process actually closes — see
		// app.beforeClose's doc comment (review fix, step 4a round 2 #1).
		OnBeforeClose: app.beforeClose,
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		logger.Error("application error", "error", err)
		os.Exit(1)
	}
}
