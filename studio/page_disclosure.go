package main

// The plain-language lines of the allow prompt for an HTML page (H6): what the
// page's host is, each declared capability, the macOS network disclosure and
// the credential warning. The wording lives in internal/artifacts so the CLI
// says the same; this file only adds what Studio knows (the platform, the
// configured model provider).

import (
	"runtime"

	"github.com/SandwichLabs/vibe/internal/artifacts"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

// PageDisclosureLine is one declared capability with its plain line.
type PageDisclosureLine struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

// PageDisclosure is the text of the allow prompt around the file list.
type PageDisclosure struct {
	Host         string               `json:"host"`
	Capabilities []PageDisclosureLine `json:"capabilities"`
	// Network is the macOS disclosure, "" elsewhere and for a static page.
	Network    string `json:"network"`
	Credential string `json:"credential"`
	// AskProvider names the configured model for `ask` ("" when none is set).
	AskProvider string `json:"askProvider"`
}

// buildPageDisclosure is the prompt text for a page's status on the given OS.
func buildPageDisclosure(st workspace.ArtifactViewStatus, goos, askTarget string) PageDisclosure {
	d := PageDisclosure{Host: artifacts.HostLine(st.PageKind), Credential: artifacts.CredentialLine, AskProvider: askTarget, Capabilities: []PageDisclosureLine{}}
	if st.PageKind != artifacts.PageScripted {
		return d
	}
	for _, c := range st.Capabilities {
		d.Capabilities = append(d.Capabilities, PageDisclosureLine{Name: c, Text: artifacts.CapabilityLine(c, askTarget)})
	}
	if goos == "darwin" {
		d.Network = artifacts.NetworkDisclosure
	}
	return d
}

// pageDisclosure is PageDisclosure's logic: the model named is the one the
// agent already uses, when it is usable.
func (a *App) pageDisclosure(st workspace.ArtifactViewStatus) PageDisclosure {
	a.mu.RLock()
	cfg := a.llmCfg
	a.mu.RUnlock()
	target := ""
	if a.askConfigured() {
		target = cfg.AskTarget()
	}
	return buildPageDisclosure(st, runtime.GOOS, target)
}

// ArtifactViewStatusView is a custom view's status as the frontend gets it: the
// workspace's status (flattened) plus, for an HTML page, the allow prompt's text.
type ArtifactViewStatusView struct {
	workspace.ArtifactViewStatus
	Disclosure *PageDisclosure `json:"disclosure,omitempty"`
}

// statusView adds the page disclosure to a status read.
func (a *App) statusView(st workspace.ArtifactViewStatus) *ArtifactViewStatusView {
	v := &ArtifactViewStatusView{ArtifactViewStatus: st}
	if st.Kind == artifacts.ViewHTML && st.Error == "" {
		d := a.pageDisclosure(st)
		v.Disclosure = &d
	}
	return v
}
