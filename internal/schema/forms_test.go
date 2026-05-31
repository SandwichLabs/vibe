package schema_test

import (
	"path/filepath"
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/sandwich-labs/vibe-app/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFormStoreBuilderPropertiesRoundTrip proves the Form Builder (parity W2)
// field properties — Placeholder / Required / Visibility / Validation / Help —
// persist through SaveForm → GetForm against a real .vibe container, and that
// the additive columns survive Save + Close + reopen.
func TestFormStoreBuilderPropertiesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "forms.vibe")

	c, err := container.Create(path,
		container.WithPassphrase(testPassphrase),
		container.WithLogger(testLogger()),
		container.WithTitle("Forms Test"),
	)
	require.NoError(t, err)

	require.True(t, tableExists(t, c, "_vibe_forms"),
		"_vibe_forms table should exist after migration")

	store := schema.NewFormStore(c.SQLiteDB())

	spec := schema.FormSpec{
		ID:        "service_call",
		Title:     "Service call",
		Table:     "service_calls",
		Archetype: "master_detail",
		Fields: []schema.FormFieldSpec{
			{
				Name:        "technician",
				Caption:     "Technician",
				ControlType: "text",
				Placeholder: "Pick from team",
				Required:    true,
				Visibility:  "Always show",
				Validation:  "Must exist in Technicians",
				Help:        "Assign the tech who took the call.",
			},
			// A field with only the original triple set — proves the additive
			// properties default cleanly and don't corrupt legacy-shaped fields.
			{Name: "issue", Caption: "Issue reported", ControlType: "textarea"},
		},
	}
	require.NoError(t, store.SaveForm(spec))

	got, err := store.GetForm("service_call")
	require.NoError(t, err)
	require.Len(t, got.Fields, 2)

	tech := got.Fields[0]
	assert.Equal(t, "Technician", tech.Caption)
	assert.Equal(t, "Pick from team", tech.Placeholder)
	assert.True(t, tech.Required)
	assert.Equal(t, "Always show", tech.Visibility)
	assert.Equal(t, "Must exist in Technicians", tech.Validation)
	assert.Equal(t, "Assign the tech who took the call.", tech.Help)

	issue := got.Fields[1]
	assert.Equal(t, "Issue reported", issue.Caption)
	assert.False(t, issue.Required)
	assert.Empty(t, issue.Placeholder)

	// Edit the label (Publish) and confirm the bumped version persists the new
	// caption — the W2 acceptance "edit a label + Publish → reopen".
	spec.Fields[0].Caption = "Assigned technician"
	require.NoError(t, store.SaveForm(spec))

	// Survive Save + Close + reopen.
	require.NoError(t, c.Save())
	require.NoError(t, c.Close())

	reopened, err := container.Open(path,
		container.WithPassphrase(testPassphrase),
		container.WithLogger(testLogger()),
	)
	require.NoError(t, err)
	defer reopened.Close() //nolint:errcheck

	store2 := schema.NewFormStore(reopened.SQLiteDB())
	after, err := store2.GetForm("service_call")
	require.NoError(t, err)
	assert.Equal(t, "Assigned technician", after.Fields[0].Caption)
	assert.Equal(t, "Must exist in Technicians", after.Fields[0].Validation)
}
