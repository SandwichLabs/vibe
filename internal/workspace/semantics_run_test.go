package workspace

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/engine"
	"github.com/SandwichLabs/vibe/internal/queries"
	"github.com/SandwichLabs/vibe/internal/semantics"
)

type defErr struct{ msg string }

func (e defErr) Error() string         { return e.msg }
func (defErr) DefinitionProblem() bool { return true }

// vetRunner records which statements were vetted and which were run.
type vetRunner struct {
	vetted, ran []string
	vetErr      func(sql string) error
}

func (v *vetRunner) RunReadOnlySQLIn(_ context.Context, _, sql string, _ map[string]any, _ int) (*engine.ResultSet, bool, error) {
	v.ran = append(v.ran, sql)
	return &engine.ResultSet{Columns: []engine.Column{{Name: "n"}}, Rows: [][]any{{int64(0)}}}, false, nil
}

func (v *vetRunner) VetReadOnlySQLIn(_ context.Context, _, sql string, _ map[string]any) error {
	v.vetted = append(v.vetted, sql)
	if v.vetErr != nil {
		return v.vetErr(sql)
	}
	return nil
}

func compileWorkspace(t *testing.T) Workspace {
	t.Helper()
	ws, _ := newWorkspace(t)
	ctx := context.Background()
	require.NoError(t, ws.Queries().Create(ctx, &queries.Query{ID: "base", Title: "base", SQL: "SELECT region, amount FROM sales WHERE amount > $floor",
		Parameters: []queries.Param{{Name: "floor", Type: "number", Default: "0"}}}))
	m, err := semantics.DecodeMetric("revenue", []byte("version: 1\nname: revenue\nquery: base\nmeasure: sum(amount)\nassertions:\n  - {kind: non_negative}\n"))
	require.NoError(t, err)
	require.NoError(t, ws.Semantics().PutMetric(ctx, m))
	e, err := semantics.DecodeEntity("customer", []byte("version: 1\nname: customer\ngrain: one row per customer\ntable: customers\nid_column: id\nassertions:\n  - {kind: not_null, column: id}\n"))
	require.NoError(t, err)
	require.NoError(t, ws.Semantics().PutEntity(ctx, e))
	return ws
}

func TestRunAssertions_CompileOnlyRunsNoDataCheck(t *testing.T) {
	ws := compileWorkspace(t)
	r := &vetRunner{}
	res, err := RunAssertions(context.Background(), ws, "", "", AssertionRunner(r), time.Now(), AssertionOptions{CompileOnly: true})
	require.NoError(t, err)
	require.Len(t, res, 2, "one compile row per entity and metric")
	assert.Empty(t, r.ran, "no statement is run")
	assert.Len(t, r.vetted, 2)
	for _, x := range res {
		assert.Equal(t, "compile", x.Check.Kind)
		assert.Equal(t, "pass", x.Status)
	}
}

func TestRunAssertions_CompileFailureSuppressesTheSubjectsRuns(t *testing.T) {
	ws := compileWorkspace(t)
	r := &vetRunner{vetErr: func(sql string) error {
		if strings.Contains(sql, "sum(amount)") {
			return defErr{"could not validate query: Binder Error: column \"amount\" not found"}
		}
		return nil
	}}
	res, err := RunAssertions(context.Background(), ws, "", "", AssertionRunner(r), time.Now(), AssertionOptions{})
	require.NoError(t, err)
	got := map[string]string{}
	for _, x := range res {
		got[x.Check.Subject+"/"+x.Check.Kind] = x.Status
	}
	assert.Equal(t, map[string]string{
		"customer/compile": "pass", "customer/not_null": "pass",
		"revenue/compile": "fail", "revenue/non_negative": "error",
	}, got)
	require.Len(t, r.ran, 1, "only the compiling entity's data check ran")
	assert.Contains(t, r.ran[0], "customers")
}

type slowVetter struct{ vetRunner }

func (s *slowVetter) VetReadOnlySQLIn(ctx context.Context, _, _ string, _ map[string]any) error {
	<-ctx.Done()
	// The policy flattens the database's error to text: the chain is lost.
	return errors.New("could not validate query: Binder Error: interrupted")
}

// A vet that ran out of time says nothing about the definition: not a fail.
func TestReadOnlyVetFunc_TimeoutIsNotADefinitionProblem(t *testing.T) {
	vet := ReadOnlyVetFunc(&slowVetter{})
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	// The outer context is not the one that expires first in the product; use a
	// short outer deadline to stand in for CheckStatementTimeout.
	err := vet(ctx, "", "SELECT 1", nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}
