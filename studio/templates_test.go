package main

import (
	"testing"

	"github.com/SandwichLabs/vibe/internal/templates"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestApp_ListTemplates asserts the binding returns the registered starter
// templates with both a name and a human-readable description for the cards.
func TestApp_ListTemplates(t *testing.T) {
	app := NewApp(testLogger())

	got, err := app.ListTemplates()
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(got), 3, "at least 3 starter templates should be exposed")

	byName := make(map[string]string, len(got))
	for _, tmpl := range got {
		assert.NotEmpty(t, tmpl.Name, "each template must expose a name")
		assert.NotEmpty(t, tmpl.Description, "each template must expose a description for the card")
		byName[tmpl.Name] = tmpl.Description
	}
	for _, name := range templates.List() {
		assert.Contains(t, byName, name, "ListTemplates should surface registered template %q", name)
	}
	assert.Contains(t, byName, "order-entry")
}
