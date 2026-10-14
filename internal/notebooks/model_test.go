package notebooks

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmpty(t *testing.T) {
	nb := Empty("My Notebook")
	assert.Equal(t, SchemaVersion, nb.SchemaVersion)
	assert.Equal(t, "My Notebook", nb.Title)
	assert.NotEmpty(t, nb.ID)
	assert.Empty(t, nb.Cells)
	assert.NotEmpty(t, nb.UpdatedAt)
}

func TestEmpty_DefaultTitle(t *testing.T) {
	nb := Empty("")
	assert.Equal(t, "Untitled notebook", nb.Title)
}

func TestNewCell(t *testing.T) {
	sqlCell, err := NewCell(CellSQL)
	require.NoError(t, err)
	assert.Equal(t, "table", sqlCell.View)
	assert.False(t, sqlCell.ViewPinned)

	mdCell, err := NewCell(CellMarkdown)
	require.NoError(t, err)
	assert.Equal(t, CellMarkdown, mdCell.Type)

	a2uiCell, err := NewCell(CellA2UI)
	require.NoError(t, err)
	require.NotNil(t, a2uiCell.Page)
	assert.Equal(t, "Untitled", a2uiCell.Page.Title)
	// A freshly created a2ui cell must itself pass Coerce/Validate — a2ui.Validate
	// rejects zero components, so NewCell must seed a placeholder or the cell
	// would fail to save the moment it round-trips through Coerce.
	nb := Empty("t")
	nb.Cells = append(nb.Cells, a2uiCell)
	b, err := json.Marshal(nb)
	require.NoError(t, err)
	coerced, warnings, err := Coerce(b)
	require.NoError(t, err)
	assert.Empty(t, warnings)
	require.Len(t, coerced.Cells, 1)

	_, err = NewCell("bogus")
	assert.Error(t, err)
}

func TestCoerce_RoundTrip(t *testing.T) {
	raw := map[string]any{
		"schemaVersion": 1,
		"id":            "nb_1",
		"title":         "Sales",
		"updatedAt":     "2026-01-01T00:00:00Z",
		"cells": []any{
			map[string]any{"id": "c1", "type": "sql", "source": "SELECT 1", "view": "table", "viewPinned": true, "label": "one"},
			map[string]any{"id": "c2", "type": "markdown", "source": "# hi"},
		},
	}
	nb, warnings, err := Coerce(raw)
	require.NoError(t, err)
	assert.Empty(t, warnings)
	assert.Equal(t, "nb_1", nb.ID)
	assert.Equal(t, "Sales", nb.Title)
	require.Len(t, nb.Cells, 2)
	assert.Equal(t, CellSQL, nb.Cells[0].Type)
	assert.True(t, nb.Cells[0].ViewPinned)
	assert.Equal(t, CellMarkdown, nb.Cells[1].Type)
}

func TestCoerce_DropsUnknownCellType(t *testing.T) {
	raw := map[string]any{
		"cells": []any{
			map[string]any{"id": "c1", "type": "form"},
			map[string]any{"id": "c2", "type": "sql", "source": "SELECT 1"},
		},
	}
	nb, warnings, err := Coerce(raw)
	require.NoError(t, err)
	require.Len(t, nb.Cells, 1)
	assert.Equal(t, "c2", nb.Cells[0].ID)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "unknown type")
}

func TestCoerce_ClampsUnknownFields(t *testing.T) {
	raw := map[string]any{
		"cells": []any{
			map[string]any{
				"id": "c1", "type": "sql", "source": "SELECT 1",
				"maliciousField": "<script>alert(1)</script>",
				"style":          "background: url(javascript:alert(1))",
			},
		},
	}
	nb, _, err := Coerce(raw)
	require.NoError(t, err)
	require.Len(t, nb.Cells, 1)
	b, err := json.Marshal(nb.Cells[0])
	require.NoError(t, err)
	assert.NotContains(t, string(b), "maliciousField")
	assert.NotContains(t, string(b), "script")
}

func TestCoerce_ClampsView(t *testing.T) {
	raw := map[string]any{
		"cells": []any{
			map[string]any{"id": "c1", "type": "sql", "source": "SELECT 1", "view": "pie"},
		},
	}
	nb, warnings, err := Coerce(raw)
	require.NoError(t, err)
	assert.Equal(t, "table", nb.Cells[0].View)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "unknown view")
}

func TestCoerce_RejectsNewerSchemaVersion(t *testing.T) {
	raw := map[string]any{
		"schemaVersion": 999,
		"cells":         []any{},
	}
	_, _, err := Coerce(raw)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "newer version")
}

func TestCoerce_RejectsMissingCellsArray(t *testing.T) {
	_, _, err := Coerce(map[string]any{"title": "x"})
	assert.Error(t, err)
}

func TestCoerce_RejectsNonObject(t *testing.T) {
	_, _, err := Coerce([]any{1, 2, 3})
	assert.Error(t, err)
}

func TestCoerce_AcceptsJSONString(t *testing.T) {
	raw := `{"title": "From JSON", "cells": [{"id": "c1", "type": "markdown", "source": "hi"}]}`
	nb, _, err := Coerce(raw)
	require.NoError(t, err)
	assert.Equal(t, "From JSON", nb.Title)
	require.Len(t, nb.Cells, 1)
}

func TestCoerce_InvalidJSONString(t *testing.T) {
	_, _, err := Coerce(`{not json`)
	assert.Error(t, err)
}

func TestCoerce_ValidatesA2UIPage(t *testing.T) {
	raw := map[string]any{
		"cells": []any{
			map[string]any{
				"id": "c1", "type": "a2ui",
				"page": map[string]any{
					"title": "Dash",
					"components": []any{
						map[string]any{"id": "s1", "type": "stat", "label": "Total", "value": 42.0},
					},
				},
			},
		},
	}
	nb, warnings, err := Coerce(raw)
	require.NoError(t, err)
	assert.Empty(t, warnings)
	require.Len(t, nb.Cells, 1)
	require.NotNil(t, nb.Cells[0].Page)
	assert.Equal(t, "Dash", nb.Cells[0].Page.Title)
}

func TestCoerce_DropsInvalidA2UIPage(t *testing.T) {
	raw := map[string]any{
		"cells": []any{
			map[string]any{
				"id": "c1", "type": "a2ui",
				"page": map[string]any{"title": "Dash", "components": []any{
					map[string]any{"id": "s1", "type": "not-a-real-type"},
				}},
			},
			map[string]any{"id": "c2", "type": "markdown", "source": "still here"},
		},
	}
	nb, warnings, err := Coerce(raw)
	require.NoError(t, err)
	require.Len(t, nb.Cells, 1)
	assert.Equal(t, "c2", nb.Cells[0].ID)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "invalid page")
}

func TestCoerce_CapsCellCount(t *testing.T) {
	cells := make([]any, maxCells+10)
	for i := range cells {
		cells[i] = map[string]any{"type": "markdown", "source": "x"}
	}
	nb, warnings, err := Coerce(map[string]any{"cells": cells})
	require.NoError(t, err)
	assert.Len(t, nb.Cells, maxCells)
	require.NotEmpty(t, warnings)
	assert.Contains(t, warnings[0], "cap")
}

func TestCoerce_CapsSourceSize(t *testing.T) {
	big := strings.Repeat("a", maxCellSourceBytes+100)
	raw := map[string]any{
		"cells": []any{map[string]any{"id": "c1", "type": "markdown", "source": big}},
	}
	nb, warnings, err := Coerce(raw)
	require.NoError(t, err)
	assert.Len(t, nb.Cells[0].Source, maxCellSourceBytes)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "truncated")
}

func TestCoerce_WrappedFileKind(t *testing.T) {
	raw := map[string]any{
		"kind":          FileKind,
		"schemaVersion": 1,
		"notebook": map[string]any{
			"id": "nb_1", "title": "wrapped", "cells": []any{},
		},
	}
	nb, _, err := Coerce(raw)
	require.NoError(t, err)
	assert.Equal(t, "wrapped", nb.Title)
}

func TestCoerce_RejectsUnrecognizedFileKind(t *testing.T) {
	raw := map[string]any{"kind": "something/else", "cells": []any{}}
	_, _, err := Coerce(raw)
	assert.Error(t, err)
}

func TestCoerce_DemotesExcessDataBoundComponents(t *testing.T) {
	// internal/a2ui.Validate itself caps ONE page at 200 components total,
	// so the notebook-wide budget only actually bites across multiple
	// cells (see TestCoerce_DataBoundBudgetIsNotebookWide) — this proves a
	// single cell sitting exactly at both caps (200 components, all
	// sql-bound) is accepted whole, none demoted.
	components := make([]any, maxDataBoundComponents)
	for i := range components {
		components[i] = map[string]any{
			"id": fmt.Sprintf("s%d", i), "type": "stat", "label": "L",
			"sql": "SELECT 1",
		}
	}
	raw := map[string]any{
		"cells": []any{
			map[string]any{"id": "c1", "type": "a2ui", "page": map[string]any{"title": "p", "components": components}},
		},
	}
	nb, warnings, err := Coerce(raw)
	require.NoError(t, err)
	require.Len(t, nb.Cells, 1)
	assert.Empty(t, warnings)

	dataBound := 0
	for _, c := range nb.Cells[0].Page.Components {
		if _, ok := c.Fields["sql"]; ok {
			dataBound++
		}
	}
	assert.Equal(t, maxDataBoundComponents, dataBound)
}

func TestCoerce_DataBoundBudgetIsNotebookWide(t *testing.T) {
	// Two a2ui cells, 150 sql-bound components each (300 total) — the cap
	// applies across the WHOLE notebook, not per cell.
	makePage := func(prefix string, n int) map[string]any {
		components := make([]any, n)
		for i := range components {
			components[i] = map[string]any{"id": fmt.Sprintf("%s%d", prefix, i), "type": "stat", "label": "L", "sql": "SELECT 1"}
		}
		return map[string]any{"title": "p", "components": components}
	}
	raw := map[string]any{
		"cells": []any{
			map[string]any{"id": "c1", "type": "a2ui", "page": makePage("a", 150)},
			map[string]any{"id": "c2", "type": "a2ui", "page": makePage("b", 150)},
		},
	}
	nb, _, err := Coerce(raw)
	require.NoError(t, err)
	require.Len(t, nb.Cells, 2)

	total := 0
	for _, cell := range nb.Cells {
		for _, c := range cell.Page.Components {
			if _, ok := c.Fields["sql"]; ok {
				total++
			}
		}
	}
	assert.Equal(t, maxDataBoundComponents, total)
}

func TestCoerce_DuplicateCellID_GetsFreshID(t *testing.T) {
	raw := map[string]any{
		"cells": []any{
			map[string]any{"id": "dup", "type": "markdown", "source": "first"},
			map[string]any{"id": "dup", "type": "markdown", "source": "second"},
			map[string]any{"id": "dup", "type": "sql", "source": "SELECT 1"},
		},
	}
	nb, warnings, err := Coerce(raw)
	require.NoError(t, err)
	require.Len(t, nb.Cells, 3)

	ids := map[string]bool{}
	for _, c := range nb.Cells {
		assert.False(t, ids[c.ID], "cell ids must be unique after Coerce")
		ids[c.ID] = true
	}
	assert.Equal(t, "dup", nb.Cells[0].ID, "the first occurrence keeps its id")
	assert.NotEqual(t, "dup", nb.Cells[1].ID)
	assert.NotEqual(t, "dup", nb.Cells[2].ID)
	assert.GreaterOrEqual(t, len(warnings), 2)
}

func TestCoerce_HonorsWrapperSchemaVersion(t *testing.T) {
	raw := map[string]any{
		"kind": FileKind, "schemaVersion": 999,
		"notebook": map[string]any{"title": "n", "cells": []any{}},
	}
	_, _, err := Coerce(raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "newer version")
}

func TestCoerce_CapsTitleAndLabel(t *testing.T) {
	raw := map[string]any{
		"title": strings.Repeat("t", maxTitleRunes+50),
		"cells": []any{
			map[string]any{"id": "c1", "type": "sql", "source": "SELECT 1", "label": strings.Repeat("l", maxLabelRunes+10)},
		},
	}
	nb, warnings, err := Coerce(raw)
	require.NoError(t, err)
	assert.Len(t, []rune(nb.Title), maxTitleRunes)
	assert.Len(t, []rune(nb.Cells[0].Label), maxLabelRunes)
	assert.GreaterOrEqual(t, len(warnings), 2)
}

func TestCoerce_NonStringSourceWarns(t *testing.T) {
	raw := map[string]any{
		"cells": []any{
			map[string]any{"id": "c1", "type": "sql", "source": 12345},
		},
	}
	nb, warnings, err := Coerce(raw)
	require.NoError(t, err)
	assert.Equal(t, "", nb.Cells[0].Source)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "not a string")
}

func TestCoerce_CollapsedRoundTrip(t *testing.T) {
	raw := map[string]any{
		"cells": []any{
			map[string]any{"id": "c1", "type": "sql", "source": "SELECT 1", "collapsed": true},
			map[string]any{"id": "c2", "type": "markdown", "source": "# hi", "collapsed": true},
			map[string]any{
				"id": "c3", "type": "a2ui", "collapsed": true,
				"page": map[string]any{
					"title": "t",
					"components": []any{
						map[string]any{"id": "h", "type": "heading", "text": "x"},
					},
				},
			},
			map[string]any{"id": "c4", "type": "sql", "source": "SELECT 2"}, // collapsed omitted
		},
	}

	nb, warnings, err := Coerce(raw)
	require.NoError(t, err)
	assert.Empty(t, warnings)
	require.Len(t, nb.Cells, 4)
	assert.True(t, nb.Cells[0].Collapsed)
	assert.True(t, nb.Cells[1].Collapsed)
	assert.True(t, nb.Cells[2].Collapsed)
	assert.False(t, nb.Cells[3].Collapsed)

	b, err := json.Marshal(nb)
	require.NoError(t, err)
	assert.Equal(t, 3, strings.Count(string(b), `"collapsed":true`))

	// Round-trip a second time through Coerce, from the marshalled JSON.
	nb2, warnings2, err := Coerce(b)
	require.NoError(t, err)
	assert.Empty(t, warnings2)
	require.Len(t, nb2.Cells, 4)
	assert.True(t, nb2.Cells[0].Collapsed)
	assert.True(t, nb2.Cells[1].Collapsed)
	assert.True(t, nb2.Cells[2].Collapsed)
	assert.False(t, nb2.Cells[3].Collapsed)

	b2, err := json.Marshal(nb2)
	require.NoError(t, err)
	assert.Equal(t, 3, strings.Count(string(b2), `"collapsed":true`))
}

func TestCell_MarshalJSON_OmitsIrrelevantFields(t *testing.T) {
	cell := Cell{ID: "c1", Type: CellMarkdown, Source: "hi", View: "bar", ViewPinned: true}
	b, err := json.Marshal(cell)
	require.NoError(t, err)
	assert.NotContains(t, string(b), "view")
	assert.NotContains(t, string(b), "viewPinned")
}

// --- Notebook format v2 (#108) ---

func TestCoerce_V1LoadsAsV2(t *testing.T) {
	nb, warnings, err := Coerce(map[string]any{
		"schemaVersion": 1,
		"id":            "nb1", "title": "Old",
		"cells": []any{
			map[string]any{"id": "c1", "type": "sql", "source": "SELECT 1", "view": "table"},
			map[string]any{"id": "c2", "type": "a2ui", "page": map[string]any{"title": "p", "components": []any{
				map[string]any{"id": "t", "type": "table", "sql": "SELECT 1"},
			}}},
		},
	})
	require.NoError(t, err)
	assert.Empty(t, warnings)
	assert.Equal(t, 2, nb.SchemaVersion)
	assert.Equal(t, "", nb.Cells[0].Connection, "v1 cells have no connection (session default)")
	assert.NotContains(t, nb.Cells[1].Page.Components[0].Fields, "connection")
}

func TestCoerce_V2ConnectionRoundTrips(t *testing.T) {
	in := map[string]any{
		"schemaVersion": 2, "id": "nb2", "title": "New",
		"cells": []any{
			map[string]any{"id": "c1", "type": "sql", "source": "SELECT * FROM orders", "connection": "prod_pg"},
			map[string]any{"id": "c2", "type": "sql", "source": "SELECT 1"},
			map[string]any{"id": "c3", "type": "a2ui", "page": map[string]any{"title": "p", "components": []any{
				map[string]any{"id": "s", "type": "stat", "label": "n", "sql": "SELECT 1", "connection": "lake"},
			}}},
		},
	}
	nb, warnings, err := Coerce(in)
	require.NoError(t, err)
	assert.Empty(t, warnings)
	assert.Equal(t, "prod_pg", nb.Cells[0].Connection)
	assert.Equal(t, "", nb.Cells[1].Connection)
	assert.Equal(t, "lake", nb.Cells[2].Page.Components[0].Fields["connection"])

	// Serialise, coerce again: identical, and unset stays absent (no empty key).
	b, err := json.Marshal(nb)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"connection":"prod_pg"`)
	assert.Contains(t, string(b), `"connection":"lake"`)
	var generic map[string]any
	require.NoError(t, json.Unmarshal(b, &generic))
	cells, ok := generic["cells"].([]any)
	require.True(t, ok)
	cell1, ok := cells[1].(map[string]any)
	require.True(t, ok)
	_, has := cell1["connection"]
	assert.False(t, has, "an unset connection must not be written")

	nb2, warnings, err := Coerce(generic)
	require.NoError(t, err)
	assert.Empty(t, warnings)
	assert.Equal(t, nb, nb2)
}

func TestCoerce_UnknownConnectionIsKeptNotDropped(t *testing.T) {
	// Odd names survive load and save untouched; the run fails clearly.
	nb, warnings, err := Coerce(map[string]any{
		"cells": []any{
			map[string]any{"id": "c1", "type": "sql", "source": "SELECT 1", "connection": "Prod DB"},
			map[string]any{"id": "c2", "type": "a2ui", "page": map[string]any{"title": "p", "components": []any{
				map[string]any{"id": "t", "type": "table", "sql": "SELECT 1", "connection": "gone"},
			}}},
		},
	})
	require.NoError(t, err)
	assert.Empty(t, warnings)
	require.Len(t, nb.Cells, 2, "no cell is dropped")
	assert.Equal(t, "Prod DB", nb.Cells[0].Connection)
	assert.Equal(t, "gone", nb.Cells[1].Page.Components[0].Fields["connection"])
}

func TestCoerce_NonStringConnectionIsCleared(t *testing.T) {
	for _, bad := range []any{12, true, []any{"a"}} {
		nb, warnings, err := Coerce(map[string]any{
			"cells": []any{map[string]any{"id": "c1", "type": "sql", "source": "SELECT 1", "connection": bad}},
		})
		require.NoError(t, err)
		require.Len(t, nb.Cells, 1)
		assert.Equal(t, "", nb.Cells[0].Connection, "%v", bad)
		require.Len(t, warnings, 1, "%v", bad)
		assert.Contains(t, warnings[0], "connection")
	}
}

func TestSaveGet_ConnectionPersists(t *testing.T) {
	storeContract(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		nb, err := s.Create(ctx, "N")
		require.NoError(t, err)
		saved, _, err := s.Save(ctx, map[string]any{
			"id": nb.ID, "title": "N",
			"cells": []any{map[string]any{"id": "c1", "type": "sql", "source": "SELECT 1", "connection": "other"}},
		})
		require.NoError(t, err)
		assert.Equal(t, "other", saved.Cells[0].Connection)
		got, _, err := s.Get(ctx, nb.ID)
		require.NoError(t, err)
		assert.Equal(t, "other", got.Cells[0].Connection)
		assert.Equal(t, SchemaVersion, got.SchemaVersion)
	})
}

func TestCoerce_RejectsV3(t *testing.T) {
	_, _, err := Coerce(map[string]any{"schemaVersion": 3, "cells": []any{}})
	require.Error(t, err)
}

func TestCoerce_KineticViewsAndMotion(t *testing.T) {
	raw := map[string]any{
		"cells": []any{
			map[string]any{"id": "c1", "type": "sql", "source": "SELECT 1", "view": "radar",
				"motion": map[string]any{"easing": "custom", "speed": 2.0, "stiffness": 190.0}},
			map[string]any{"id": "c2", "type": "sql", "source": "SELECT 1", "view": "sankey",
				"motion": map[string]any{"easing": "wobble"}},
		},
	}
	nb, warnings, err := Coerce(raw)
	require.NoError(t, err)
	assert.Equal(t, "radar", nb.Cells[0].View)
	assert.Equal(t, "custom", nb.Cells[0].Motion["easing"])
	assert.Equal(t, "sankey", nb.Cells[1].View)
	assert.Nil(t, nb.Cells[1].Motion, "an invalid motion is cleared, the cell kept")
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "invalid motion")

	b, err := json.Marshal(nb.Cells[0])
	require.NoError(t, err)
	assert.Contains(t, string(b), `"motion":{"easing":"custom"`)
	b, err = json.Marshal(nb.Cells[1])
	require.NoError(t, err)
	assert.NotContains(t, string(b), "motion")
}
