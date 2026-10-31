package hub

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrepareForPerson_GuardsTheTypedText(t *testing.T) {
	var seen []string
	guard := func(_ context.Context, s string) error {
		seen = append(seen, s)
		if s == "INSTALL x" {
			return errors.New("INSTALL is not available here")
		}
		return nil
	}
	exp, err := PrepareForPerson(context.Background(), nil, nil, nil, PersonRun{SQL: "SELECT 1", Params: map[string]any{"a": 1}, Guard: guard})
	require.NoError(t, err)
	assert.Equal(t, "SELECT 1", exp.SQL)
	assert.Equal(t, map[string]any{"a": 1}, exp.Params)
	assert.Equal(t, []string{"SELECT 1"}, seen, "nothing expanded: the typed text is checked once")

	_, err = PrepareForPerson(context.Background(), nil, nil, nil, PersonRun{SQL: "INSTALL x", Guard: guard})
	require.Error(t, err)

	exp, err = PrepareForPerson(context.Background(), nil, nil, nil, PersonRun{SQL: "SELECT 2"})
	require.NoError(t, err)
	assert.Equal(t, "SELECT 2", exp.SQL, "no guard skips the checks")
}
