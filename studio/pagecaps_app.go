package main

// The App side of a page's capabilities (step P5): the services pagecaps.go
// calls. They are helpers, not bindings: a page reaches them only through its
// window's pipe, and only for the capabilities its manifest declared.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/SandwichLabs/assay/internal/llm"
	"github.com/SandwichLabs/assay/internal/pagestate"
	"github.com/SandwichLabs/assay/internal/workspace"
)

var errSaveBusy = errors.New("a save dialog is already open")

// maxActivity bounds the in-memory activity log.
const maxActivity = 100

// pageAppState is the App's capability state: one save dialog at a time, and
// the activity log.
type pageAppState struct {
	dialog sync.Mutex
	mu     sync.Mutex
	log    []ActivityEntry
}

func (p *pageAppState) add(e ActivityEntry) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.log = append(p.log, e)
	if len(p.log) > maxActivity {
		p.log = p.log[len(p.log)-maxActivity:]
	}
}

func (p *pageAppState) entries() []ActivityEntry {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]ActivityEntry(nil), p.log...)
}

// pageServices builds the services of one artifact's windows. The artifact is
// fixed here, by Studio: nothing a page sends names one.
func (a *App) pageServices(ws workspace.Workspace, name string) pageServices {
	return pageServices{
		state: func() (*pagestate.Store, error) {
			return a.pages.stateStore(ws.Root(), name, func(err error) {
				a.logger.Debug("page state not saved", "artifact", name, "error", err)
			})
		},
		save:     a.savePageFile,
		ask:      a.askForPage,
		askOn:    func(hash string) bool { return ws.PageAskEnabled(name, hash) },
		activity: a.recordActivity,
	}
}

// savePageFile is the native save dialog with the name filled in. It returns
// "" when the person cancels. Only one is open at a time.
func (a *App) savePageFile(ctx context.Context, title, filename, ext string) (string, error) {
	if !a.pageApp.dialog.TryLock() {
		return "", errSaveBusy
	}
	defer a.pageApp.dialog.Unlock()
	if a.ctx == nil {
		return "", errors.New("no window")
	}
	a.logger.Info("a page opened the save dialog", "title", title, "filename", filename)
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Save a file from the page " + title,
		DefaultFilename: filename,
		Filters:         []runtime.FileFilter{{DisplayName: strings.ToUpper(strings.TrimPrefix(ext, ".")) + " (*" + ext + ")", Pattern: "*" + ext}},
	})
	if err != nil {
		return "", err
	}
	return path, ctx.Err()
}

const askPreamble = "You are answering one request from a page the person opened in Assay. You have no tools and no access to files, data or the web. Answer in plain text."

// askForPage is a single completion on the configured provider: no tools, no
// history, at most 4,000 tokens. configured is false when no provider is set.
func (a *App) askForPage(ctx context.Context, system, prompt string) (string, bool, error) {
	a.mu.RLock()
	cfg := a.llmConfig("read-only", true)
	a.mu.RUnlock()
	if !llm.Configured(cfg) {
		return "", false, nil
	}
	if strings.TrimSpace(system) != "" {
		system = askPreamble + "\n\n" + system
	} else {
		system = askPreamble
	}
	text, err := llm.NewRunner(nil, a.logger).Complete(ctx, cfg, system, prompt, llm.MaxCompletionTokens)
	return text, true, err
}

// recordActivity adds a line to the agent activity log and tells the UI.
func (a *App) recordActivity(e ActivityEntry) {
	a.pageApp.add(e)
	a.logger.Info("agent activity", "source", e.Source, "artifact", e.Name, "kind", e.Kind, "outcome", e.Outcome, "prompt_bytes", e.PromptBytes, "reply_bytes", e.ReplyBytes)
	a.emitEvent("agent:activity", map[string]any{"source": e.Source, "name": e.Name, "kind": e.Kind, "at": e.At.Format(time.RFC3339), "outcome": e.Outcome})
}

// askConfigured reports whether a model provider is usable for `ask` now.
func (a *App) askConfigured() bool {
	a.mu.RLock()
	cfg := a.llmConfig("read-only", true)
	a.mu.RUnlock()
	return llm.Configured(cfg)
}
