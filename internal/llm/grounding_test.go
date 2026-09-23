package llm

import "testing"

func TestAssertsFact(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"OK, I added a notebook cell for that.", false},
		{"Sure thing!", false},
		{"There are 42 rows in the sales table.", true},
		{"Every row in the table has a null region.", true},
		{"None of the customers have an email on file.", true},
		{"I created the table `orders` with columns `id`, `total`.", false},
		{"Roughly how many rows? I'd estimate about 5000 rows in total.", true},
		{"```sql\nSELECT COUNT(*) FROM t WHERE year = 2024\n```", false},
	}
	for _, c := range cases {
		if got := assertsFact(c.text); got != c.want {
			t.Errorf("assertsFact(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

func TestUsedGroundingTool(t *testing.T) {
	assertUsed := func(t *testing.T, steps []ToolStep, want bool) {
		t.Helper()
		if got := usedGroundingTool(steps); got != want {
			t.Errorf("usedGroundingTool(%+v) = %v, want %v", steps, got, want)
		}
	}

	assertUsed(t, nil, false)
	assertUsed(t, []ToolStep{{Tool: "introspect_schema"}}, false)
	assertUsed(t, []ToolStep{{Tool: "run_query"}}, true)
	assertUsed(t, []ToolStep{{Tool: "preview_query"}}, true)
	assertUsed(t, []ToolStep{{Tool: "run_query", IsError: true}}, false)
	assertUsed(t, []ToolStep{{Tool: "present_data", Input: `{"title":"x"}`}}, false)
	assertUsed(t, []ToolStep{{Tool: "present_data", Input: `{"sql":"SELECT 1"}`}}, true)
	assertUsed(t, []ToolStep{{Tool: "describe_table", Output: `{"table":"t","columns":[]}`}}, false)
	assertUsed(t, []ToolStep{{Tool: "describe_table", Output: `{"sample":[{"id":1}]}`}}, true)
}

// TestMightBeParamRejection_OnlyNamesReasoningParam is a review-fix test
// (#70 round 2): the old, loose version also matched generic phrases like
// "extra fields not permitted" or "unrecognized" that have nothing to do
// with reasoning_effort — a malformed tool argument or an unrelated 400
// would have wrongly triggered the reasoning-effort retry. Only text that
// clearly names the parameter counts now.
func TestMightBeParamRejection_OnlyNamesReasoningParam(t *testing.T) {
	if mightBeParamRejection(nil) {
		t.Error("nil error must not be treated as a param rejection")
	}
	cases := map[string]bool{
		"unknown error":                                  false,
		"connection refused":                             false,
		"extra fields not permitted":                     false, // no longer a match: names no specific param
		"unrecognized field \"top_p\"":                   false, // names a DIFFERENT param
		"invalid arguments for add_query_cell: bad json": false, // a tool-call error, not a request rejection
		`400: unrecognized field "reasoning_effort"`:     true,
		"unsupported parameter: reasoning_effort":        true,
		"400 Bad Request: reasoning is not supported":    true,
	}
	for msg, want := range cases {
		err := errTest(msg)
		if got := mightBeParamRejection(err); got != want {
			t.Errorf("mightBeParamRejection(%q) = %v, want %v", msg, got, want)
		}
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }

func TestWroteThisTurn(t *testing.T) {
	assertWrote := func(t *testing.T, steps []ToolStep, want bool) {
		t.Helper()
		if got := wroteThisTurn(steps); got != want {
			t.Errorf("wroteThisTurn(%+v) = %v, want %v", steps, got, want)
		}
	}

	assertWrote(t, nil, false)
	assertWrote(t, []ToolStep{{Tool: "introspect_schema"}}, false)
	assertWrote(t, []ToolStep{{Tool: "create_record"}}, true)
	assertWrote(t, []ToolStep{{Tool: "create_record", IsError: true}}, false)
	assertWrote(t, []ToolStep{{Tool: "update_record"}}, true)
	assertWrote(t, []ToolStep{{Tool: "delete_record"}}, true)
	assertWrote(t, []ToolStep{{Tool: "bulk_insert"}}, true)
	assertWrote(t, []ToolStep{{Tool: "create_table"}}, true)
	assertWrote(t, []ToolStep{{Tool: "add_query_cell"}}, true)
	// run_query/preview_query are excluded here by design: a successful call
	// to either already makes usedGroundingTool true, which short-circuits
	// maybeGroundText before wroteThisTurn is ever consulted.
	assertWrote(t, []ToolStep{{Tool: "run_query"}}, false)
	assertWrote(t, []ToolStep{{Tool: "preview_query"}}, false)
}
