package repl

import (
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/queries"
	"github.com/SandwichLabs/vibe/internal/session"
)

// output is what the console printed since the prompt that preceded cmd.
func (f *fixture) output(cmd string) string {
	f.t.Helper()
	f.run(cmd + "\r")
	out := f.out.String()
	i := strings.LastIndex(out, cmd+"\r\n")
	require.GreaterOrEqual(f.t, i, 0, out)
	rest := out[i+len(cmd)+2:]
	return strings.TrimSuffix(strings.ReplaceAll(rest, "\r\n", "\n"), prompt)
}

func TestDotCommands(t *testing.T) {
	cases := []struct {
		cmd  string
		want string
	}{
		{".mode", "current output mode: duckbox\n"},
		{".mode json", ""},
		{".mode nope", "Error: mode should be one of: box csv duckbox json line markdown\n"},
		{".timer maybe", "Invalid Command Error: Invalid usage of command '.timer'\n\nUsage: '.timer on|off'\n"},
		{".headers", "Invalid Command Error: Invalid usage of command '.headers'\n\nUsage: '.headers on|off'\n"},
		{".maxrows", "current max rows: 40\n"},
		{".maxrows 5", ""},
		{".maxrows abc", "Invalid Command Error: Invalid usage of command '.maxrows'\n\nUsage: '.maxrows COUNT'\n"},
		{".maxrows -2", "Invalid Command Error: Invalid usage of command '.maxrows'\n\nUsage: '.maxrows COUNT'\n"},
		{".maxrows 0", ""},
		{".maxwidth", "current max width: 0\n"},
		{".maxwidth -1", "Invalid Command Error: Invalid usage of command '.maxwidth'\n\nUsage: '.maxwidth COUNT'\n"},
		{".nullvalue", "Invalid Command Error: Invalid usage of command '.nullvalue'\n\nUsage: '.nullvalue STRING'\n"},
		{".nullvalue (none)", ""},
		{".columns", ""},
		{".rows", ""},
		{".read", "Error: no saved queries here\n"},
		{".save x", "Error: no saved queries here\n"},
		{".grid", "Error: .grid opens the last result in the Query Explorer; it is not available in this console yet.\n"},
		{".export", "Error: .export is not available in this console yet; use the Query Explorer's export.\n"},
		{".shell ls", "Error: .shell is not available: the console runs SQL on the workspace hub, never a shell.\n"},
		{".system ls", "Error: .system is not available: the console runs SQL on the workspace hub, never a shell.\n"},
		{".open x.db", "Error: .open is not available: the console is bound to this workspace's hub; connections are defined in vibe.yaml.\n"},
		{".load x", "Error: .load is not available: Vibe loads the DuckDB extensions a connection needs itself.\n"},
		{".cd /", "Error: .cd is not available: the console has no working directory.\n"},
		{".import a b", "Error: .import is not available here: use Import (vibe import, or Studio's Import) instead.\n"},
		{".output f", "Error: .output is not available: the console does not write files; use .export (Query Explorer) instead.\n"},
		{".once f", "Error: .once is not available: the console does not write files; use .export (Query Explorer) instead.\n"},
		{".log f", "Error: .log is not available here.\n"},
		{".bogus", "Unknown Command Error: Unrecognized command 'bogus'\nRun '.help' for more information.\n"},
	}
	for _, c := range cases {
		t.Run(c.cmd, func(t *testing.T) {
			f := newSession(t, nil)
			f.waitFor(prompt)
			assert.Equal(t, c.want, f.output(c.cmd))
		})
	}
}

func TestDotSettingsTakeEffect(t *testing.T) {
	f := newSession(t, nil)
	f.waitFor(prompt)
	f.output(".mode csv")
	assert.Equal(t, "a,b\n1,NULL\n", f.output("select 1 as a, cast(NULL as varchar) as b;"))
	f.output(".nullvalue -")
	assert.Equal(t, "a,b\n1,-\n", f.output("select 1 as a, cast(NULL as varchar) as b;"))
	f.output(".headers off")
	assert.Equal(t, "1,-\n", f.output("select 1 as a, cast(NULL as varchar) as b;"))
	f.output(".mode json")
	assert.Equal(t, "[{\"a\":1,\"b\":null}]\n", f.output("select 1 as a, cast(NULL as varchar) as b;"))
	f.output(".mode line")
	assert.Equal(t, "    a = 1\n    b = -\n", f.output("select 1 as a, cast(NULL as varchar) as b;"))
	f.output(".mode markdown")
	assert.Equal(t, "| a | b |\n|--:|---|\n| 1 | - |\n", f.output("select 1 as a, cast(NULL as varchar) as b;"))
	f.output(".mode box")
	f.output(".columns")
	assert.Contains(t, f.output("select 1 as a, 'x' as b;"), "│ Column │ Row 1 │")
}

func TestTimerLine(t *testing.T) {
	f := newSession(t, nil)
	f.waitFor(prompt)
	f.output(".timer on")
	assert.Contains(t, f.output("select 1;"), "Run Time (s): real 0.000\n")
	assert.Contains(t, f.output("select * from nope;"), "Run Time (s): real 0.000\n", "also after an error, as DuckDB")
}

func TestDotTablesAndSchema(t *testing.T) {
	f := newSession(t, nil)
	f.waitFor(prompt)
	f.run("create table orders(id int);\r")
	f.run("create table order_lines(id int);\r")
	f.run("create view customers as select 1 as id;\r")
	assert.Equal(t, "customers    order_lines  orders\n", f.output(".tables"))
	assert.Equal(t, "order_lines  orders\n", f.output(".tables order%"))
	assert.Equal(t, "orders\n", f.output(".tables orders"))
	assert.Equal(t, "CREATE TABLE orders(id INTEGER);\n", f.output(".schema orders"))
	assert.Contains(t, f.output(".schema"), "CREATE VIEW customers")
}

func TestLikeMatch(t *testing.T) {
	for _, c := range []struct {
		p, n string
		want bool
	}{
		{"%", "", true}, {"%", "abc", true}, {"a%", "abc", true}, {"a_c", "abc", true}, {"a_c", "abbc", false},
		{"%c", "abc", true}, {"%b%", "abc", true}, {"abc", "abc", true}, {"abc", "ab", false}, {"a%%c", "ac", true}, {"é_", "éa", true},
	} {
		assert.Equal(t, c.want, likeMatch(c.p, c.n), "%q ~ %q", c.p, c.n)
	}
}

func TestConnSwitches(t *testing.T) {
	f := newSession(t, nil)
	f.lazyFile("other")
	f.waitFor(prompt)

	assert.Contains(t, f.output(".conn"), "current: memory\n")
	assert.Contains(t, f.output(".conn"), "other  not-connected")

	assert.Empty(t, f.output(".conn other"), "it connects first")
	assert.Equal(t, "other", f.s.Current())
	assert.Contains(t, f.output("select current_database() as c;"), "other")
	st, _ := f.sess.Status("other")
	assert.Equal(t, session.StatusOK, st.Kind)
	assert.Contains(t, f.output(".tables"), "t")

	// A USE typed by hand moves it too.
	f.run("use memory;\r")
	assert.Equal(t, "memory", f.s.Current())

	assert.Equal(t, "Error: unknown connection \"nope\"\n", f.output(".conn nope"))
	assert.Equal(t, "Invalid Command Error: Invalid usage of command '.conn'\n\nUsage: '.conn [NAME]'\n", f.output(".conn a b"))
}

func TestConnSwitchCallsTheHook(t *testing.T) {
	var calls []string
	var mu sync.Mutex
	f := newSession(t, func(o *Options) {
		o.Hooks.AfterRun = func(_ map[string]session.StatusKind, sql string) {
			mu.Lock()
			calls = append(calls, sql)
			mu.Unlock()
		}
	})
	f.lazyFile("other")
	f.waitFor(prompt)
	f.output(".conn other")
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{`USE "other"`}, calls)
}

func TestInitialConnection(t *testing.T) {
	f := newHub(t)
	f.lazyFile("other")
	require.NoError(t, f.open(func(o *Options) { o.Connection = "other" }))
	f.waitFor(prompt)
	assert.Equal(t, "other", f.s.Current())
	assert.Contains(t, f.output("select * from t;"), "│     7 │")

	g := newHub(t)
	assert.Error(t, g.open(func(o *Options) { o.Connection = "nope" }))
	assert.Error(t, g.open(func(o *Options) { o.Connection = "bad name" }))
}

func TestReadAndSave(t *testing.T) {
	store := queries.NewFileStore(t.TempDir())
	f := newSession(t, func(o *Options) { o.Queries = store })
	f.waitFor(prompt)

	assert.Equal(t, "Error: nothing to save: run a statement first\n", f.output(".save first"))
	f.run("select 41 + 1 as answer;\r")
	assert.True(t, strings.HasPrefix(f.output(".save The answer"), "saved as queries/"))
	all, err := store.List(t.Context())
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, "The answer", all[0].Title)
	assert.Equal(t, "select 41 + 1 as answer", all[0].SQL)
	assert.Empty(t, all[0].Connection, "the default catalog is not recorded")

	out := f.output(".read The answer")
	assert.Contains(t, out, prompt+"select 41 + 1 as answer\n")
	assert.Contains(t, out, "│     42 │")
	assert.Equal(t, "Error: no saved query \"missing\"\n", f.output(".read missing"))

	require.NoError(t, store.Create(t.Context(), &queries.Query{Title: "p", SQL: "select $a, $b", Parameters: []queries.Param{}}))
	assert.Equal(t, "Error: saved query \"p\" takes parameters (a, b); run it from the Query Explorer\n", f.output(".read p"))
}

func TestSaveNeverOffersAnAttachOrASet(t *testing.T) {
	store := queries.NewFileStore(t.TempDir())
	f := newSession(t, func(o *Options) { o.Queries = store })
	f.waitFor(prompt)
	f.run("select 1 as ok;\r")
	f.run("set threads=1;\r")
	f.run("attach ':memory:' as x1;\r")
	f.output(".save kept")
	all, err := store.List(t.Context())
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, "select 1 as ok", all[0].SQL, "a statement that never goes in history is never saved either")
}

func TestTabCompletionSeam(t *testing.T) {
	f := newSession(t, func(o *Options) {
		o.Complete = func(r CompleteRequest) []string {
			if strings.HasSuffix(r.Text[:r.Pos], "ord") {
				return []string{"orders", "order_lines"}
			}
			return nil
		}
	})
	f.waitFor(prompt)
	f.run("select * from ord\t;\r") // two candidates: the common prefix "order"
	assert.Contains(t, f.out.String(), "from order")
}
