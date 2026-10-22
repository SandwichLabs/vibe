package main

// Page windows of scripted HTML pages (step P4). The bindings are thin: the
// manager is pagehost.go. A window is opened only by the person, from Studio,
// for a page they allowed; no tool, MCP method or page can open one.

import (
	"github.com/SandwichLabs/vibe/internal/artifacts"
)

// PageWindowSupport says why page windows cannot open here ("" when they can):
// no native web view in this build, no Wayland session, a system that cannot
// isolate the window, macOS before 14. The text is fixed.
func (a *App) PageWindowSupport() string {
	if err := a.pages.preflight(); err != nil {
		return err.Error()
	}
	return ""
}

// OpenArtifactPageWindow opens the named scripted page in its own window. query
// is the raw query string of the page's parameter controls ("" for the
// defaults) and theme the --vibe-* roles of the window the person is looking at,
// from which the page's host stylesheet is built. The page must be allowed right
// now, at the hash the person saw. The window's end (stopped, changed, closed) is
// the pagewindow:state event.
func (a *App) OpenArtifactPageWindow(name, query string, theme map[string]string) (*PageWindowInfo, error) {
	ws, _, _, err := a.snapshot()
	if err != nil {
		return nil, err
	}
	return a.pages.open(pageOpen{
		ws: ws, name: name, query: query, theme: theme, base: a.ctxOrBackground(),
		runner:  func(m *artifacts.Manifest) artifacts.PageRunner { return a.pageRunner(name, m) },
		allowed: func(hash string) bool { return a.pageStillAllowed(ws, name, hash) },
	})
}

// ClosePageWindow closes the named artifact's page window, if one is open.
func (a *App) ClosePageWindow(name string) {
	a.pages.closeArtifact(name, pageStateClosed)
}
