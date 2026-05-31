// Package llm wires Vibe's in-app conversational agent to an OpenAI-compatible
// LLM endpoint (e.g. a local llama-server at http://localhost:8080/v1) using the
// charmbracelet/fantasy agent framework.
//
// This is the real "wizard layer" agent loop: the model is given a set of tools
// that read and mutate the open .vibe (list/describe tables, run read-only
// queries, create tables) and fantasy drives the multi-step tool-calling loop
// until the model produces a final answer.
//
// The package depends only on a narrow Deps interface so it never imports the
// studio package (avoiding an import cycle); studio/app.go supplies an adapter.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/openaicompat"
)

// Sandbox modes mirror the agent sandbox levels used elsewhere in Vibe.
const (
	SandboxReadOnly  = "read-only"
	SandboxReadWrite = "read-write"
	SandboxFull      = "full"
)

// maxSteps bounds the tool-calling loop so a confused model can't spin forever.
const maxSteps = 8

// Config configures the LLM endpoint and the agent's capability level.
type Config struct {
	Endpoint string // OpenAI-compatible base URL, e.g. http://localhost:8080/v1
	Model    string // model id to request (many local servers ignore this)
	APIKey   string // bearer token; local servers often accept any non-empty value
	Sandbox  string // read-only | read-write | full — gates mutating tools
}

// ColumnSpec describes one column for create_table.
type ColumnSpec struct {
	Name       string
	Type       string
	PrimaryKey bool
	NotNull    bool
}

// TableSpec describes a table for create_table.
type TableSpec struct {
	Name    string
	Columns []ColumnSpec
}

// QueryResult is the column/row shape returned by Deps.Query.
type QueryResult struct {
	Columns []string
	Rows    [][]string
}

// Deps is the narrow capability surface the agent's tools call into. studio
// implements this against the open container/engine.
type Deps interface {
	ListTables(ctx context.Context) ([]string, error)
	DescribeTable(ctx context.Context, table string) (string, error)
	Query(ctx context.Context, sql string) (*QueryResult, error)
	CreateTable(ctx context.Context, spec TableSpec) error
}

// ToolStep is a single tool invocation surfaced to the UI so the panel can show
// the agent's tool use ("read schema", "ran SQL", "created table").
type ToolStep struct {
	Tool   string `json:"tool"`
	Input  string `json:"input"`
	Output string `json:"output"`
}

// ChatResult is the final agent answer plus the tool steps it took.
type ChatResult struct {
	Text  string     `json:"text"`
	Steps []ToolStep `json:"steps"`
}

// Runner builds and runs the fantasy agent for a single chat turn.
type Runner struct {
	deps   Deps
	logger *slog.Logger
}

// NewRunner constructs a Runner over the given dependencies.
func NewRunner(deps Deps, logger *slog.Logger) *Runner {
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{deps: deps, logger: logger}
}

const systemPrompt = `You are the Vibe wizard — the agent layer of Vibe, a local-first, encrypted application container that is the spiritual successor to Microsoft Access 97. You help a small-business builder design and query a relational database stored in a single .vibe file backed by DuckDB.

Use the provided tools to inspect the schema before answering questions about data, and to make changes when asked. Prefer reading the schema (list_tables, describe_table) before writing SQL. Keep answers concise and practical. When you create or change schema, briefly confirm what you did.`

// recorder accumulates tool steps during a single Generate call. Tools may run
// in parallel, so it is mutex-guarded.
type recorder struct {
	mu    sync.Mutex
	steps []ToolStep
}

func (r *recorder) add(s ToolStep) {
	r.mu.Lock()
	r.steps = append(r.steps, s)
	r.mu.Unlock()
}

// Chat runs one agent turn. pinnedContext, if non-empty, is prepended to the
// user message so the agent knows what the user is currently looking at (the
// "pin context" affordance in the panel).
func (r *Runner) Chat(ctx context.Context, cfg Config, message, pinnedContext string) (*ChatResult, error) {
	if strings.TrimSpace(message) == "" {
		return nil, fmt.Errorf("empty message")
	}
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		return nil, fmt.Errorf("no LLM endpoint configured")
	}
	apiKey := cfg.APIKey
	if strings.TrimSpace(apiKey) == "" {
		// Local OpenAI-compatible servers usually accept any non-empty key; the
		// underlying SDK rejects an empty one.
		apiKey = "sk-local"
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = "local"
	}

	provider, err := openaicompat.New(
		openaicompat.WithBaseURL(endpoint),
		openaicompat.WithAPIKey(apiKey),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM provider: %w", err)
	}
	languageModel, err := provider.LanguageModel(ctx, model)
	if err != nil {
		return nil, fmt.Errorf("failed to load model %q: %w", model, err)
	}

	rec := &recorder{}
	tools := r.buildTools(cfg.Sandbox, rec)

	agent := fantasy.NewAgent(languageModel,
		fantasy.WithSystemPrompt(systemPrompt),
		fantasy.WithTools(tools...),
		fantasy.WithStopConditions(fantasy.StepCountIs(maxSteps)),
	)

	prompt := message
	if pc := strings.TrimSpace(pinnedContext); pc != "" {
		prompt = fmt.Sprintf("[Context the user has pinned]\n%s\n\n%s", pc, message)
	}

	result, err := agent.Generate(ctx, fantasy.AgentCall{Prompt: prompt})
	if err != nil {
		return nil, fmt.Errorf("agent error: %w", err)
	}

	return &ChatResult{
		Text:  result.Response.Content.Text(),
		Steps: rec.steps,
	}, nil
}

// buildTools returns the tool set permitted by the sandbox level. Read tools are
// always available; mutating tools require read-write or full.
func (r *Runner) buildTools(sandbox string, rec *recorder) []fantasy.AgentTool {
	tools := []fantasy.AgentTool{
		r.listTablesTool(rec),
		r.describeTableTool(rec),
		r.queryTool(rec),
	}
	if sandbox == SandboxReadWrite || sandbox == SandboxFull {
		tools = append(tools, r.createTableTool(rec))
	}
	return tools
}

type emptyInput struct{}

func (r *Runner) listTablesTool(rec *recorder) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		"list_tables",
		"List the names of all user tables in the open .vibe database.",
		func(ctx context.Context, _ emptyInput, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			tables, err := r.deps.ListTables(ctx)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			out := strings.Join(tables, ", ")
			if out == "" {
				out = "(no tables yet)"
			}
			rec.add(ToolStep{Tool: "list_tables", Output: out})
			return fantasy.NewTextResponse(out), nil
		},
	)
}

type describeInput struct {
	Table string `json:"table" description:"The name of the table to describe."`
}

func (r *Runner) describeTableTool(rec *recorder) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		"describe_table",
		"Describe a table's columns and types.",
		func(ctx context.Context, in describeInput, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			desc, err := r.deps.DescribeTable(ctx, in.Table)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			rec.add(ToolStep{Tool: "describe_table", Input: in.Table, Output: desc})
			return fantasy.NewTextResponse(desc), nil
		},
	)
}

type queryInput struct {
	SQL string `json:"sql" description:"A read-only SQL SELECT query to run against DuckDB."`
}

func (r *Runner) queryTool(rec *recorder) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		"query",
		"Run a read-only SQL SELECT query and return the rows. Only SELECT/WITH/SHOW/DESCRIBE/PRAGMA statements are allowed.",
		func(ctx context.Context, in queryInput, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if !isReadOnlySQL(in.SQL) {
				msg := "only read-only queries (SELECT/WITH/SHOW/DESCRIBE/PRAGMA) are allowed by this tool"
				rec.add(ToolStep{Tool: "query", Input: in.SQL, Output: "rejected: " + msg})
				return fantasy.NewTextErrorResponse(msg), nil
			}
			res, err := r.deps.Query(ctx, in.SQL)
			if err != nil {
				rec.add(ToolStep{Tool: "query", Input: in.SQL, Output: "error: " + err.Error()})
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			out := formatResult(res)
			rec.add(ToolStep{Tool: "query", Input: in.SQL, Output: fmt.Sprintf("%d row(s)", len(res.Rows))})
			return fantasy.NewTextResponse(out), nil
		},
	)
}

type createTableInput struct {
	Name    string `json:"name" description:"The name of the new table."`
	Columns []struct {
		Name       string `json:"name" description:"Column name."`
		Type       string `json:"type" description:"DuckDB column type, e.g. TEXT, INTEGER, DOUBLE, DATE, BOOLEAN."`
		PrimaryKey bool   `json:"primary_key" description:"Whether this column is the primary key."`
		NotNull    bool   `json:"not_null" description:"Whether this column is NOT NULL."`
	} `json:"columns" description:"The columns of the new table."`
}

func (r *Runner) createTableTool(rec *recorder) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		"create_table",
		"Create a new table in the .vibe database with the given columns.",
		func(ctx context.Context, in createTableInput, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			spec := TableSpec{Name: in.Name}
			for _, c := range in.Columns {
				spec.Columns = append(spec.Columns, ColumnSpec{
					Name:       c.Name,
					Type:       c.Type,
					PrimaryKey: c.PrimaryKey,
					NotNull:    c.NotNull,
				})
			}
			inputJSON, _ := json.Marshal(in)
			if err := r.deps.CreateTable(ctx, spec); err != nil {
				rec.add(ToolStep{Tool: "create_table", Input: string(inputJSON), Output: "error: " + err.Error()})
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			out := fmt.Sprintf("created table %q with %d column(s)", spec.Name, len(spec.Columns))
			rec.add(ToolStep{Tool: "create_table", Input: string(inputJSON), Output: out})
			return fantasy.NewTextResponse(out), nil
		},
	)
}

// isReadOnlySQL is a conservative allow-list guard for the query tool.
func isReadOnlySQL(sql string) bool {
	s := strings.ToLower(strings.TrimSpace(sql))
	s = strings.TrimPrefix(s, "(")
	for _, p := range []string{"select", "with", "show", "describe", "desc", "pragma", "explain", "table ", "from "} {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// formatResult renders a small result set as a compact text table for the model.
func formatResult(res *QueryResult) string {
	if res == nil || len(res.Columns) == 0 {
		return "(no results)"
	}
	var b strings.Builder
	b.WriteString(strings.Join(res.Columns, " | "))
	b.WriteString("\n")
	const maxRows = 50
	for i, row := range res.Rows {
		if i >= maxRows {
			fmt.Fprintf(&b, "... (%d more rows)\n", len(res.Rows)-maxRows)
			break
		}
		b.WriteString(strings.Join(row, " | "))
		b.WriteString("\n")
	}
	return b.String()
}
