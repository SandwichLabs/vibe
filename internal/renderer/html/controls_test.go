package html

// TDD RED — Phase 1 / unit P1.4: "Bound controls renderer (combo hidden-PK,
// lookup, checkbox, date, ControlTip)" (parity plan §1.4).
//
// This is a REAL integration test: it stands up a real encrypted .vibe container
// (SQLCipher) + a real DuckDB engine, seeds a `statuses` lookup table and an
// `orders` table with a `status_id` foreign key, records Access-style field
// metadata (control_type=combo, lookup_sql, value_col, display_col, control_tip),
// and renders the bound controls through the production renderer. No mocking of
// the engine or the container.
//
// It asserts the acceptance behavior of plan §1.4:
//   - a combo box renders the *display_col* text but submits the *value_col* id
//     (the Access "show 'Visa', store 2" pattern); options are resolved
//     server-side by executing lookup_sql via the engine (no SQL leaks to client)
//   - the option matching the bound record's stored value_col is marked selected
//   - the ControlTip surfaces as the control's title/help text
//   - a boolean field renders an <input type="checkbox">
//   - a date field renders <input type="date">
//   - the SAME control implementation (controls.go) is exercised by both the
//     {form} renderer and the datasheet inline-edit cell (no duplication)

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/sandwich-labs/vibe-app/internal/engine"
	"github.com/sandwich-labs/vibe-app/internal/renderer"
	"github.com/sandwich-labs/vibe-app/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const controlsTestPassphrase = "controls-test-passphrase-12345"

// newControlsTestFixture builds a real .vibe container + DuckDB engine, seeds a
// `statuses` lookup table and an `orders` table with a `status_id` FK, and
// records the Access-style field metadata for status_id (combo bound to the
// statuses lookup), a boolean flag, and a date column. It returns the engine and
// the loaded field-meta map keyed by column name.
func newControlsTestFixture(t *testing.T) (*engine.Manager, map[string]schema.FieldMeta) {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	dir := t.TempDir()
	path := filepath.Join(dir, "controls.vibe")
	c, err := container.Create(path,
		container.WithPassphrase(controlsTestPassphrase),
		container.WithLogger(logger),
		container.WithTitle("Controls Test"),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	ddb := c.DuckDBDB()
	require.NotNil(t, ddb)

	_, err = ddb.Exec(`CREATE TABLE statuses (id INTEGER PRIMARY KEY, label VARCHAR);`)
	require.NoError(t, err, "create statuses table")
	_, err = ddb.Exec(`INSERT INTO statuses (id, label) VALUES (1, 'Active'), (2, 'Closed');`)
	require.NoError(t, err, "seed statuses")

	_, err = ddb.Exec(`CREATE TABLE orders (
		id INTEGER PRIMARY KEY,
		status_id INTEGER,
		shipped BOOLEAN,
		ordered_on DATE,
		notes VARCHAR
	);`)
	require.NoError(t, err, "create orders table")

	// Record Access-style field metadata for the orders table.
	store := schema.NewStore(c.SQLiteDB())
	require.NoError(t, store.SetFieldMeta("orders", "status_id", schema.FieldMeta{
		TableName:   "orders",
		ColumnName:  "status_id",
		Caption:     "Status",
		ControlType: "combo",
		LookupSQL:   "SELECT id, label FROM statuses",
		ValueCol:    "id",
		DisplayCol:  "label",
		ControlTip:  "Pick a status",
	}))
	require.NoError(t, store.SetFieldMeta("orders", "shipped", schema.FieldMeta{
		TableName:   "orders",
		ColumnName:  "shipped",
		Caption:     "Shipped",
		ControlType: "checkbox",
	}))
	require.NoError(t, store.SetFieldMeta("orders", "ordered_on", schema.FieldMeta{
		TableName:   "orders",
		ColumnName:  "ordered_on",
		Caption:     "Ordered On",
		ControlType: "date",
	}))

	metas, err := loadFieldMetaMap(store, "orders")
	require.NoError(t, err)

	eng := engine.NewManager(ddb, engine.WithLogger(logger))
	return eng, metas
}

// formFieldFromMeta is a small helper that builds the renderer.FormField the
// renderer consumes from a stored schema.FieldMeta. The production renderer must
// carry the control_type / value_col / display_col / control_tip / lookup_sql
// through to controls.go.
func formFieldFromMeta(m schema.FieldMeta) renderer.FormField {
	return renderer.FormField{
		Name:        m.ColumnName,
		Label:       m.Caption,
		ControlType: m.ControlType,
		LookupSQL:   m.LookupSQL,
		ValueCol:    m.ValueCol,
		DisplayCol:  m.DisplayCol,
		ControlTip:  m.ControlTip,
		Required:    m.Required,
	}
}

// TestBoundControls drives the bound-control renderer end-to-end: a combo box
// with hidden-PK/display semantics, a checkbox, and a date picker, all resolved
// from real field metadata + a real lookup query.
func TestBoundControls(t *testing.T) {
	eng, metas := newControlsTestFixture(t)
	r := NewRenderer(slog.New(slog.NewTextHandler(io.Discard, nil)))

	// An options resolver backed by the REAL engine — combo options must be
	// resolved server-side from lookup_sql so SQL never leaks to the client.
	resolver := NewEngineOptionResolver(eng)

	statusField := formFieldFromMeta(metas["status_id"])
	shippedField := formFieldFromMeta(metas["shipped"])
	dateField := formFieldFromMeta(metas["ordered_on"])

	// The bound record currently stores status_id = 2 ("Closed").
	boundStatus := int64(2)

	cfg := &renderer.FormConfig{
		Table:  "orders",
		FormID: "order_form",
		PageID: "orders",
		Fields: []renderer.FormField{
			func() renderer.FormField { f := statusField; f.Default = boundStatus; return f }(),
			shippedField,
			dateField,
		},
	}

	formHTML, err := r.RenderFormWithOptions(context.Background(), cfg, resolver)
	require.NoError(t, err)

	// --- Combo: shows display_col text, submits value_col id, options from lookup_sql ---
	assert.Contains(t, formHTML, `<option value="2"`,
		"combo option value must be the value_col id (store 2)")
	assert.Contains(t, formHTML, `>Closed</option>`,
		"combo option text must be the display_col label (show 'Closed')")
	assert.Contains(t, formHTML, `<option value="1"`, "combo must include the other lookup row")
	assert.Contains(t, formHTML, `>Active</option>`, "combo must show the other lookup label")

	// The option matching the bound record's stored value_col must be selected.
	assert.Regexp(t, `<option value="2"[^>]*\bselected\b[^>]*>Closed</option>`, formHTML,
		"the option for the bound record's stored value_col (2 => Closed) must be selected")

	// SQL must NOT leak to the client.
	assert.NotContains(t, formHTML, "SELECT id, label FROM statuses",
		"lookup SQL must be resolved server-side and never emitted to the client")

	// --- ControlTip surfaces as a title/help attribute on the control ---
	assert.Contains(t, formHTML, `title="Pick a status"`,
		"control_tip must surface as a title/help attribute")

	// --- Checkbox + date picker render from control_type ---
	assert.Contains(t, formHTML, `type="checkbox"`, "boolean field must render a checkbox")
	assert.Contains(t, formHTML, `type="date"`, "date field must render a date picker")

	// --- The SAME controls.go must back the datasheet inline-edit cell ---
	// Rendering the combo as a datasheet edit cell must produce the same combo
	// (display text + hidden value) — proving no duplicate control implementation.
	editCellHTML, err := r.RenderEditCell(context.Background(),
		func() renderer.FormField { f := statusField; f.Default = boundStatus; return f }(),
		resolver)
	require.NoError(t, err)
	assert.Contains(t, editCellHTML, `<option value="2"`,
		"datasheet inline-edit cell must reuse the combo control (value_col id)")
	assert.Contains(t, editCellHTML, `>Closed</option>`,
		"datasheet inline-edit cell must show the display_col label")
	assert.Regexp(t, `<option value="2"[^>]*\bselected\b`, editCellHTML,
		"datasheet inline-edit combo must preselect the bound value")
	assert.True(t, strings.Contains(editCellHTML, `name="status_id"`),
		"datasheet inline-edit combo must submit under the bound column name")
}
