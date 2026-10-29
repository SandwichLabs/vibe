package repl

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/usersql"
)

// TestRunPathParity: the console takes the gates the Query Explorer takes
// (usersql.CheckInteractive: same text), and calls the AfterRun hook exactly
// once for each statement that passed them (a failed run included, as
// RunQuery's afterRun is), never for a refusal.
func TestRunPathParity(t *testing.T) {
	type call struct {
		before map[string]session.StatusKind
		sql    string
	}
	var mu sync.Mutex
	var calls []call
	f := newSession(t, func(o *Options) {
		o.Hooks.AfterRun = func(b map[string]session.StatusKind, sql string) {
			mu.Lock()
			calls = append(calls, call{b, sql})
			mu.Unlock()
		}
	})
	f.waitFor(prompt)

	cases := []struct {
		stmt    string
		refused bool
	}{
		{"INSTALL httpfs", true},
		{"CREATE SECRET s (TYPE s3)", true},
		{"SET s3_url_compatibility_mode=true", true},
		{"SELECT 1", false},
		{"CREATE TABLE p(x INT)", false},
		{"SELECT * FROM nope", false},
	}
	wantCalls := 0
	for _, c := range cases {
		before := session.StatusSnapshot(f.sess)
		gate := usersql.CheckInteractive(t.Context(), f.sess, c.stmt)
		f.run(c.stmt + ";\r")
		if c.refused {
			require.Error(t, gate, c.stmt)
			assert.Contains(t, f.out.String(), "Error: "+gate.Error(), c.stmt)
		} else {
			require.NoError(t, gate, c.stmt)
			wantCalls++
		}
		mu.Lock()
		require.Len(t, calls, wantCalls, c.stmt)
		if !c.refused {
			assert.Equal(t, c.stmt, calls[wantCalls-1].sql)
			assert.Equal(t, before, calls[wantCalls-1].before, c.stmt)
		}
		mu.Unlock()
	}
}

// A statement that names a lazily attached connection dials it, exactly as the
// editor's does (engine.ExecuteOn is the editor's run path on a held
// connection), and the hook sees the status move.
func TestLazyAttachThroughTheConsole(t *testing.T) {
	var mu sync.Mutex
	var moved bool
	f := newHub(t)
	f.lazyFile("other")
	require.NoError(t, f.open(func(o *Options) {
		o.Hooks.AfterRun = func(before map[string]session.StatusKind, _ string) {
			mu.Lock()
			defer mu.Unlock()
			moved = before["other"] != session.StatusOK
		}
	}))
	f.waitFor(prompt)
	st, _ := f.sess.Status("other")
	require.NotEqual(t, session.StatusOK, st.Kind)
	f.run("select * from other.main.t;\r")
	assert.Contains(t, f.out.String(), "│     7 │")
	st, _ = f.sess.Status("other")
	assert.Equal(t, session.StatusOK, st.Kind)
	mu.Lock()
	defer mu.Unlock()
	assert.True(t, moved)
}
