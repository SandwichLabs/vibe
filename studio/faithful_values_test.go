package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Faithful value rendering (engine normalization + Studio bindings) ---

func mustExec(t *testing.T, app *App, sqlStr string) {
	t.Helper()
	_, err := app.ExecuteQuery(sqlStr)
	require.NoError(t, err, sqlStr)
}

// probeTableDDL creates a table exercising every type category this test
// file touches, with one row whose values are chosen to hit the wire
// contract's edge cases (a BIGINT PK past 2^53, a DECIMAL, a UUID, ...).
const probeTableDDL = `CREATE TABLE probe (
	id BIGINT PRIMARY KEY,
	amt DECIMAL(18,4),
	d DATE,
	ts TIMESTAMP,
	u UUID,
	note VARCHAR
)`

// TestGetTableData_FaithfulTypes proves GetTableData reports the real
// DuckDB type names (not Go scan-type names) and that UUID/DECIMAL cells
// come back as the wire contract's canonical text/exact number instead of
// mojibake or a lossy float.
func TestGetTableData_FaithfulTypes(t *testing.T) {
	app := newOpenApp(t)
	mustExec(t, app, probeTableDDL)
	mustExec(t, app, `INSERT INTO probe VALUES
		(9007199254740993, 12.5, '2025-04-06', '2025-04-06 13:45:00',
		 '4ac7a9e9-607c-4c8a-84f3-843f0191e3fd', NULL)`)

	td, err := app.GetTableData("probe", 1, 50, "", "")
	require.NoError(t, err)
	require.Equal(t, []string{"id", "amt", "d", "ts", "u", "note"}, td.Columns)
	require.Equal(t, []string{"BIGINT", "DECIMAL(18,4)", "DATE", "TIMESTAMP", "UUID", "VARCHAR"}, td.Types)
	require.Len(t, td.Rows, 1)

	row := td.Rows[0]
	assert.Equal(t, "9007199254740993", row[0], "BIGINT above 2^53 must cross as its exact decimal string")
	assert.Equal(t, json.Number("12.5000"), row[1], "DECIMAL(18,4) must keep its scale")
	assert.Equal(t, "2025-04-06", row[2])
	assert.Equal(t, "2025-04-06 13:45:00", row[3])
	assert.Equal(t, "4ac7a9e9-607c-4c8a-84f3-843f0191e3fd", row[4], "UUID must render as canonical text, not mojibake")
	assert.Nil(t, row[5])
}

// TestExecuteQuery_FaithfulTypes proves ExecuteQuery's ColumnInfo.Type
// carries the DuckDB type name and that the JSON-marshalled result contains
// DECIMAL/BIGINT text exactly (no []byte-to-string loop needed any more —
// normalization owns it).
func TestExecuteQuery_FaithfulTypes(t *testing.T) {
	app := newOpenApp(t)
	mustExec(t, app, probeTableDDL)
	mustExec(t, app, `INSERT INTO probe VALUES
		(9007199254740993, 15675.30, '2025-04-06', '2025-04-06 13:45:00',
		 '4ac7a9e9-607c-4c8a-84f3-843f0191e3fd', NULL)`)

	// amt is DECIMAL(18,4): 15675.30 stored at that scale is 15675.3000.
	res, err := app.ExecuteQuery("SELECT id, amt FROM probe")
	require.NoError(t, err)
	require.Len(t, res.Columns, 2)
	assert.Equal(t, "BIGINT", res.Columns[0].Type)
	assert.Equal(t, "DECIMAL(18,4)", res.Columns[1].Type)

	b, err := json.Marshal(res)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"9007199254740993"`)
	assert.Contains(t, string(b), "15675.3000")
	assert.Regexp(t, `^(<1 ms|\d+ ms|\d+\.\d\d s)$`, res.Duration)
}

// TestHumanDuration pins the run bar's three duration bands.
func TestHumanDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{500 * time.Microsecond, "<1 ms"},
		{1 * time.Millisecond, "1 ms"},
		{312 * time.Millisecond, "312 ms"},
		{999 * time.Millisecond, "999 ms"},
		{1 * time.Second, "1.00 s"},
		{1500 * time.Millisecond, "1.50 s"},
		{12345 * time.Millisecond, "12.35 s"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, humanDuration(tc.d), tc.d.String())
	}
}

// TestRunA2UIQuery_FaithfulTypes proves RunA2UIQuery's new Types field is
// populated and parallel to Columns.
func TestRunA2UIQuery_FaithfulTypes(t *testing.T) {
	app := newOpenApp(t)
	mustExec(t, app, probeTableDDL)
	mustExec(t, app, `INSERT INTO probe VALUES
		(1, 12.50, '2025-04-06', '2025-04-06 13:45:00', uuid(), 'x')`)

	res, err := app.RunA2UIQuery("SELECT id, amt FROM probe")
	require.NoError(t, err)
	require.Equal(t, []string{"id", "amt"}, res.Columns)
	require.Equal(t, []string{"BIGINT", "DECIMAL(18,4)"}, res.Types)
}

// TestUpdateRowDeleteRow_BigIntPKRoundTrip is the pinned regression: before
// this fix, a BIGINT PK above 2^53 reached UpdateRow/DeleteRow
// as a rounded float64 (Wails decoding a JS number), so an edit meant for
// row 9007199254740993 silently hit 9007199254740992 instead. The grid now
// sends that PK as a string (GetTableData's wire value), and CAST(? AS
// BIGINT) parses the string exactly — this test drives UpdateRow/DeleteRow
// with that string PK directly, the same shape the frontend now sends.
func TestUpdateRowDeleteRow_BigIntPKRoundTrip(t *testing.T) {
	app := newOpenApp(t)
	mustExec(t, app, probeTableDDL)
	mustExec(t, app, `INSERT INTO probe (id, note) VALUES
		(9007199254740992, 'lo'), (9007199254740993, 'hi')`)

	_, err := app.UpdateRow("probe", "id", "9007199254740993", map[string]any{"note": "updated"})
	require.NoError(t, err)

	res, err := app.ExecuteQuery("SELECT id, note FROM probe ORDER BY id")
	require.NoError(t, err)
	require.Len(t, res.Rows, 2)
	assert.Equal(t, "lo", res.Rows[0][1], "the neighboring row must be untouched")
	assert.Equal(t, "updated", res.Rows[1][1], "the exact targeted row must be updated")

	_, err = app.DeleteRow("probe", "id", "9007199254740993")
	require.NoError(t, err)

	res, err = app.ExecuteQuery("SELECT id FROM probe ORDER BY id")
	require.NoError(t, err)
	require.Len(t, res.Rows, 1)
	assert.Equal(t, "9007199254740992", res.Rows[0][0], "only the exact targeted row must be deleted")
}
