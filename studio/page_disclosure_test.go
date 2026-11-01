package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/SandwichLabs/assay/internal/workspace"
)

func TestBuildPageDisclosure(t *testing.T) {
	st := workspace.ArtifactViewStatus{Kind: "html", PageKind: "scripted", Capabilities: []string{"ask", "state"}}
	d := buildPageDisclosure(st, "darwin", "Anthropic (m)")
	assert.Contains(t, d.Network, "network connection")
	assert.Len(t, d.Capabilities, 2)
	assert.Contains(t, d.Capabilities[0].Text, "to Anthropic (m)")
	assert.Contains(t, d.Credential, "never asks for a password")

	d = buildPageDisclosure(st, "linux", "")
	assert.Empty(t, d.Network, "the disclosure is macOS only")
	assert.Contains(t, d.Capabilities[0].Text, "whatever model provider is set up later")

	d = buildPageDisclosure(workspace.ArtifactViewStatus{Kind: "html", PageKind: "static"}, "darwin", "x")
	assert.Empty(t, d.Network, "a static page has no window")
	assert.Empty(t, d.Capabilities)
	assert.Contains(t, d.Host, "no script")
}
