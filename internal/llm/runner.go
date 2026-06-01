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
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/google"
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

// Provider identifiers for the LLM backend.
const (
	ProviderLocal     = "local"     // OpenAI-compatible server (default)
	ProviderAnthropic = "anthropic" // Anthropic Claude
	ProviderGemini    = "gemini"    // Google Gemini
)

// Config configures the LLM provider/endpoint and the agent's capability level.
type Config struct {
	Provider string // local | anthropic | gemini (default local)
	Endpoint string // OpenAI-compatible base URL for the local provider
	Model    string // model id to request; empty = per-provider default
	APIKey   string // bearer token / cloud API key
	Sandbox  string // read-only | read-write | full — gates mutating tools
}

// defaultModelFor returns a sensible default model id for a provider when none
// is configured. These are overridable via config; cloud ids in particular may
// need updating as providers release new models.
func defaultModelFor(provider string) string {
	switch provider {
	case ProviderAnthropic:
		return "claude-sonnet-4-5"
	case ProviderGemini:
		return "gemini-2.5-flash"
	default:
		return "local"
	}
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

// FormFieldDef is one bound control on a form.
type FormFieldDef struct {
	Name        string
	Caption     string
	ControlType string
}

// FormDef describes a form for create_form.
type FormDef struct {
	Title     string
	Table     string
	Archetype string // columnar | continuous | datasheet | master_detail
	Fields    []FormFieldDef
	// Optional master-detail subform.
	SubformTable     string
	SubformChildKey  string
	SubformParentKey string
}

// QueryDef describes a named query for save_query.
type QueryDef struct {
	Title       string
	SQL         string
	Description string
}

// ReportColumnDef is one column in a report's detail band.
type ReportColumnDef struct {
	Field   string
	Caption string
}

// ReportDef describes a banded report for create_report.
type ReportDef struct {
	Title   string
	Source  string // the report's SQL
	GroupBy []string
	Columns []ReportColumnDef
}

// RelationshipDef describes a foreign key for add_relationship.
type RelationshipDef struct {
	ChildTable   string
	ChildColumn  string
	ParentTable  string
	ParentColumn string
	OnDelete     string
}

// PageInfo is a page list entry surfaced to the agent (title + type).
type PageInfo struct {
	Title    string
	PageType string
}

// PageDef is a new page the agent authors. Content is a Vibe page document:
// markdown plus named ```sql blocks and {directive:} tags.
type PageDef struct {
	Title   string
	Content string
}

// Deps is the narrow capability surface the agent's tools call into. studio
// implements this against the open container/engine.
type Deps interface {
	ListTables(ctx context.Context) ([]string, error)
	DescribeTable(ctx context.Context, table string) (string, error)
	Query(ctx context.Context, sql string) (*QueryResult, error)
	CreateTable(ctx context.Context, spec TableSpec) error
	CreateForm(ctx context.Context, def FormDef) error
	SaveQuery(ctx context.Context, def QueryDef) error
	CreateReport(ctx context.Context, def ReportDef) error
	AddRelationship(ctx context.Context, def RelationshipDef) error
	// Pages — the interactive markdown+SQL document surface (dashboards, notes).
	// Pages are addressed by title (the agent thinks in titles, not UUIDs); the
	// adapter resolves a title to its stored id.
	ListPages(ctx context.Context) ([]PageInfo, error)
	GetPage(ctx context.Context, title string) (string, error)
	CreatePage(ctx context.Context, def PageDef) error
	UpdatePage(ctx context.Context, title, content string) error
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

const baseSystemPrompt = `You are the Vibe wizard — the agent layer of Vibe, a local-first, encrypted application container that is the spiritual successor to Microsoft Access 97. You help a small-business builder design and query a relational database stored in a single .vibe file backed by DuckDB.

Prefer reading the schema (list_tables, describe_table) before writing SQL or making changes. Keep answers concise and practical. When you create or change schema, briefly confirm what you did.`

// systemPromptFor builds a capability-aware system prompt so the model knows
// exactly what it can do given the active sandbox. Without this the model tends
// to wrongly claim it is "read-only" and ask the user to run DDL by hand,
// instead of using the create_table tool it actually has.
func systemPromptFor(sandbox string) string {
	var b strings.Builder
	b.WriteString(baseSystemPrompt)
	b.WriteString("\n\nYour available tools and what you are allowed to do:\n")
	b.WriteString("- list_tables: list the tables in the database.\n")
	b.WriteString("- describe_table: inspect a table's columns.\n")
	b.WriteString("- query: run a READ-ONLY SQL SELECT (no DDL/DML through this tool).\n")
	b.WriteString("- list_pages: list the pages (interactive dashboards/notes) in the app.\n")
	b.WriteString("- get_page: read a page's source by its title.\n")
	if sandbox == SandboxReadWrite || sandbox == SandboxFull {
		b.WriteString("- create_table: create a new table.\n")
		b.WriteString("- add_relationship: declare a foreign key linking two tables.\n")
		b.WriteString("- create_form: build a data-entry form bound to a table (columnar/continuous/datasheet/master-detail).\n")
		b.WriteString("- save_query: save a named SQL query for reuse.\n")
		b.WriteString("- create_page: author an interactive PAGE — a markdown document with embedded ```sql blocks and {directive:} tags (tables, charts, metrics).\n")
		b.WriteString("- update_page: rewrite an existing page's content (look it up first with get_page).\n")
		b.WriteString("- create_report: build a banded, print-style REPORT from a SQL query.\n")
		b.WriteString("YOU HAVE FULL WRITE ACCESS to this database — you can build whole applications: tables, relationships, forms, queries, pages, and reports. When the user asks you to build or create something, DO IT by calling the matching tool directly — do NOT say you are read-only and do NOT ask the user to run SQL themselves. Never try to run CREATE/INSERT/UPDATE/DELETE through the read-only `query` tool; use create_table for schema changes.\n")
		b.WriteString("\nPAGE vs REPORT — do not confuse these:\n")
		b.WriteString("- A PAGE is an interactive, on-screen document: markdown prose plus named ```sql blocks and {directive:} tags such as {table: query=NAME}, {metric: query=NAME, label: Revenue}, {bar_chart: query=NAME, x: col, y: col}, {line_chart: query=NAME, x: col, y: col}, {value: query=NAME}. Use create_page for ANY request for a \"page\", \"dashboard\", \"screen\", \"overview\", \"notebook\", or interactive view. This is the DEFAULT for building a viewable screen.\n")
		b.WriteString("- A REPORT is a static, banded, print/export-style layout (grouping bands + detail rows). Use create_report ONLY when the user explicitly asks for a \"report\" or a printable/grouped banded layout. Never substitute create_report when the user asked for a page.\n")
		b.WriteString("\nPage authoring format — give each result set a NAME on the ```sql fence line, then render it with a directive whose arguments are COMMA-separated (it is parsed as YAML, so separate args with commas, NOT spaces):\n")
		b.WriteString("```sql by_month\nSELECT month, SUM(total) AS total FROM sales GROUP BY month ORDER BY month\n```\n")
		b.WriteString("{bar_chart: query=by_month, x: month, y: total}\n")
	} else {
		b.WriteString("\nYou currently have READ-ONLY access: you can inspect and query data but cannot modify the schema. If the user asks to create or change tables, explain that they can enable write access in the agent settings, and offer the SQL they could run.\n")
	}
	return b.String()
}

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

// Turn is one prior message in a conversation, used to give the agent
// multi-turn memory.
type Turn struct {
	Role string // "user" | "agent"
	Text string
}

// StreamEvent is one incremental update emitted during ChatStream: a text delta,
// a tool invocation starting, a tool finishing, the final answer, or an error.
type StreamEvent struct {
	Kind   string `json:"kind"` // "delta" | "tool" | "tool_done" | "final" | "error"
	Text   string `json:"text,omitempty"`
	ID     string `json:"id,omitempty"`   // tool call id, to correlate tool/tool_done
	Tool   string `json:"tool,omitempty"` // tool name
	Input  string `json:"input,omitempty"`
	Output string `json:"output,omitempty"`
}

// Emitter receives StreamEvents as the agent works.
type Emitter func(StreamEvent)

// buildAgent constructs the fantasy agent for a turn: builds the configured
// provider's model and equips the sandbox-permitted tools. The recorder
// accumulates tool steps for the final ChatResult.
func (r *Runner) buildAgent(ctx context.Context, cfg Config, rec *recorder) (fantasy.Agent, error) {
	languageModel, err := r.languageModel(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return fantasy.NewAgent(languageModel,
		fantasy.WithSystemPrompt(systemPromptFor(cfg.Sandbox)),
		fantasy.WithTools(r.buildTools(cfg.Sandbox, rec)...),
		fantasy.WithStopConditions(fantasy.StepCountIs(maxSteps)),
	), nil
}

// languageModel resolves the configured provider into a fantasy LanguageModel.
// local -> OpenAI-compatible endpoint; anthropic -> Claude; gemini -> Google.
func (r *Runner) languageModel(ctx context.Context, cfg Config) (fantasy.LanguageModel, error) {
	provider := strings.TrimSpace(cfg.Provider)
	if provider == "" {
		provider = ProviderLocal
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = defaultModelFor(provider)
	}

	var (
		p   fantasy.Provider
		err error
	)
	switch provider {
	case ProviderAnthropic:
		if strings.TrimSpace(cfg.APIKey) == "" {
			return nil, fmt.Errorf("anthropic provider requires an API key (set ANTHROPIC_API_KEY or llm.api_key)")
		}
		p, err = anthropic.New(anthropic.WithAPIKey(cfg.APIKey))
	case ProviderGemini:
		if strings.TrimSpace(cfg.APIKey) == "" {
			return nil, fmt.Errorf("gemini provider requires an API key (set GEMINI_API_KEY or llm.api_key)")
		}
		p, err = google.New(google.WithGeminiAPIKey(cfg.APIKey))
	case ProviderLocal:
		endpoint := strings.TrimSpace(cfg.Endpoint)
		if endpoint == "" {
			return nil, fmt.Errorf("local provider requires an endpoint (llm.endpoint)")
		}
		apiKey := cfg.APIKey
		if strings.TrimSpace(apiKey) == "" {
			// Local OpenAI-compatible servers usually accept any non-empty key;
			// the underlying SDK rejects an empty one.
			apiKey = "sk-local"
		}
		p, err = openaicompat.New(
			openaicompat.WithBaseURL(endpoint),
			openaicompat.WithAPIKey(apiKey),
		)
	default:
		return nil, fmt.Errorf("unknown LLM provider %q (want local, anthropic, or gemini)", provider)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create %s provider: %w", provider, err)
	}

	languageModel, err := p.LanguageModel(ctx, model)
	if err != nil {
		return nil, fmt.Errorf("failed to load model %q: %w", model, err)
	}
	return languageModel, nil
}

// withPinned prepends the pinned context (if any) to the user message.
func withPinned(message, pinnedContext string) string {
	if pc := strings.TrimSpace(pinnedContext); pc != "" {
		return fmt.Sprintf("[Context the user has pinned]\n%s\n\n%s", pc, message)
	}
	return message
}

// buildMessages converts prior turns into fantasy messages for multi-turn
// memory. The current user message is appended by the caller.
func buildMessages(history []Turn) []fantasy.Message {
	msgs := make([]fantasy.Message, 0, len(history)+1)
	for _, t := range history {
		if strings.TrimSpace(t.Text) == "" {
			continue
		}
		if t.Role == "agent" {
			msgs = append(msgs, fantasy.Message{
				Role:    fantasy.MessageRoleAssistant,
				Content: []fantasy.MessagePart{fantasy.TextPart{Text: t.Text}},
			})
		} else {
			msgs = append(msgs, fantasy.NewUserMessage(t.Text))
		}
	}
	return msgs
}

// Chat runs one agent turn (non-streaming). Kept for the back-compat AgentChat
// binding and unit tests. pinnedContext, if non-empty, is prepended to the user
// message so the agent knows what the user is currently looking at.
func (r *Runner) Chat(ctx context.Context, cfg Config, message, pinnedContext string) (*ChatResult, error) {
	if strings.TrimSpace(message) == "" {
		return nil, fmt.Errorf("empty message")
	}
	rec := &recorder{}
	agent, err := r.buildAgent(ctx, cfg, rec)
	if err != nil {
		return nil, err
	}
	result, err := agent.Generate(ctx, fantasy.AgentCall{Prompt: withPinned(message, pinnedContext)})
	if err != nil {
		return nil, fmt.Errorf("agent error: %w", err)
	}
	return &ChatResult{Text: result.Response.Content.Text(), Steps: rec.steps}, nil
}

// ChatStream runs one agent turn with streaming. It emits incremental
// StreamEvents (text deltas, tool start/finish) via emit as the agent works,
// threading prior history for multi-turn memory, and returns the final result.
func (r *Runner) ChatStream(ctx context.Context, cfg Config, message, pinnedContext string, history []Turn, emit Emitter) (*ChatResult, error) {
	if strings.TrimSpace(message) == "" {
		return nil, fmt.Errorf("empty message")
	}
	if emit == nil {
		emit = func(StreamEvent) {}
	}
	rec := &recorder{}
	agent, err := r.buildAgent(ctx, cfg, rec)
	if err != nil {
		return nil, err
	}

	messages := buildMessages(history)
	messages = append(messages, fantasy.NewUserMessage(withPinned(message, pinnedContext)))

	var streamed strings.Builder
	result, err := agent.Stream(ctx, fantasy.AgentStreamCall{
		Messages: messages,
		OnTextDelta: func(_ string, text string) error {
			streamed.WriteString(text)
			emit(StreamEvent{Kind: "delta", Text: text})
			return nil
		},
		OnToolCall: func(tc fantasy.ToolCallContent) error {
			emit(StreamEvent{Kind: "tool", ID: tc.ToolCallID, Tool: tc.ToolName, Input: tc.Input})
			return nil
		},
		OnToolResult: func(res fantasy.ToolResultContent) error {
			out := ""
			if t, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentText](res.Result); ok {
				out = t.Text
			}
			emit(StreamEvent{Kind: "tool_done", ID: res.ToolCallID, Tool: res.ToolName, Output: out})
			return nil
		},
		OnError: func(e error) {
			emit(StreamEvent{Kind: "error", Text: e.Error()})
		},
	})
	if err != nil {
		emit(StreamEvent{Kind: "error", Text: err.Error()})
		return nil, fmt.Errorf("agent error: %w", err)
	}

	text := result.Response.Content.Text()
	if strings.TrimSpace(text) == "" {
		text = streamed.String()
	}
	emit(StreamEvent{Kind: "final", Text: text})
	return &ChatResult{Text: text, Steps: rec.steps}, nil
}

// buildTools returns the tool set permitted by the sandbox level. Read tools are
// always available; mutating tools require read-write or full.
func (r *Runner) buildTools(sandbox string, rec *recorder) []fantasy.AgentTool {
	tools := []fantasy.AgentTool{
		r.listTablesTool(rec),
		r.describeTableTool(rec),
		r.queryTool(rec),
		r.listPagesTool(rec),
		r.getPageTool(rec),
	}
	if sandbox == SandboxReadWrite || sandbox == SandboxFull {
		tools = append(tools,
			r.createTableTool(rec),
			r.createFormTool(rec),
			r.saveQueryTool(rec),
			r.createPageTool(rec),
			r.updatePageTool(rec),
			r.createReportTool(rec),
			r.addRelationshipTool(rec),
		)
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
		PrimaryKey bool   `json:"primary_key,omitempty" description:"Whether this column is the primary key."`
		NotNull    bool   `json:"not_null,omitempty" description:"Whether this column is NOT NULL."`
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

type createFormInput struct {
	Title     string `json:"title" description:"Human title for the form."`
	Table     string `json:"table" description:"The table this form is bound to."`
	Archetype string `json:"archetype" description:"Form layout: columnar, continuous, datasheet, or master_detail."`
	Fields    []struct {
		Name        string `json:"name" description:"Column name to bind."`
		Caption     string `json:"caption,omitempty" description:"Field label."`
		ControlType string `json:"control_type,omitempty" description:"Control type, e.g. text, number, date, checkbox, select."`
	} `json:"fields" description:"The fields to show on the form."`
	SubformTable     string `json:"subform_table,omitempty" description:"Optional child table for a master-detail subform."`
	SubformChildKey  string `json:"subform_child_key,omitempty" description:"Child table FK column linking to the master."`
	SubformParentKey string `json:"subform_parent_key,omitempty" description:"Master table key column the child links to."`
}

func (r *Runner) createFormTool(rec *recorder) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		"create_form",
		"Create a data-entry form bound to a table (columnar, continuous, datasheet, or master-detail with an optional subform).",
		func(ctx context.Context, in createFormInput, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			def := FormDef{
				Title:            in.Title,
				Table:            in.Table,
				Archetype:        in.Archetype,
				SubformTable:     in.SubformTable,
				SubformChildKey:  in.SubformChildKey,
				SubformParentKey: in.SubformParentKey,
			}
			for _, f := range in.Fields {
				def.Fields = append(def.Fields, FormFieldDef{Name: f.Name, Caption: f.Caption, ControlType: f.ControlType})
			}
			inputJSON, _ := json.Marshal(in)
			if err := r.deps.CreateForm(ctx, def); err != nil {
				rec.add(ToolStep{Tool: "create_form", Input: string(inputJSON), Output: "error: " + err.Error()})
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			out := fmt.Sprintf("created %s form %q on table %q", def.Archetype, def.Title, def.Table)
			rec.add(ToolStep{Tool: "create_form", Input: string(inputJSON), Output: out})
			return fantasy.NewTextResponse(out), nil
		},
	)
}

type saveQueryInput struct {
	Title       string `json:"title" description:"Name for the saved query."`
	SQL         string `json:"sql" description:"The SQL the query runs."`
	Description string `json:"description,omitempty" description:"Optional description of what the query is for."`
}

func (r *Runner) saveQueryTool(rec *recorder) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		"save_query",
		"Save a named SQL query for reuse. The SQL may be any valid DuckDB query.",
		func(ctx context.Context, in saveQueryInput, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			inputJSON, _ := json.Marshal(in)
			if err := r.deps.SaveQuery(ctx, QueryDef(in)); err != nil {
				rec.add(ToolStep{Tool: "save_query", Input: string(inputJSON), Output: "error: " + err.Error()})
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			out := fmt.Sprintf("saved query %q", in.Title)
			rec.add(ToolStep{Tool: "save_query", Input: string(inputJSON), Output: out})
			return fantasy.NewTextResponse(out), nil
		},
	)
}

type createReportInput struct {
	Title   string   `json:"title" description:"Report title."`
	Source  string   `json:"source" description:"The SQL query that produces the report's rows."`
	GroupBy []string `json:"group_by,omitempty" description:"Optional column names to group/band by, outermost first."`
	Columns []struct {
		Field   string `json:"field" description:"Column name from the source query."`
		Caption string `json:"caption,omitempty" description:"Column header."`
	} `json:"columns" description:"Columns to show in the detail band."`
}

func (r *Runner) createReportTool(rec *recorder) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		"create_report",
		"Create a banded report from a SQL query, with optional grouping and detail columns.",
		func(ctx context.Context, in createReportInput, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			def := ReportDef{Title: in.Title, Source: in.Source, GroupBy: in.GroupBy}
			for _, c := range in.Columns {
				def.Columns = append(def.Columns, ReportColumnDef{Field: c.Field, Caption: c.Caption})
			}
			inputJSON, _ := json.Marshal(in)
			if err := r.deps.CreateReport(ctx, def); err != nil {
				rec.add(ToolStep{Tool: "create_report", Input: string(inputJSON), Output: "error: " + err.Error()})
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			out := fmt.Sprintf("created report %q", in.Title)
			rec.add(ToolStep{Tool: "create_report", Input: string(inputJSON), Output: out})
			return fantasy.NewTextResponse(out), nil
		},
	)
}

func (r *Runner) listPagesTool(rec *recorder) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		"list_pages",
		"List the interactive pages (markdown+SQL dashboards/notes) in the open .vibe app, with their type.",
		func(ctx context.Context, _ emptyInput, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			pages, err := r.deps.ListPages(ctx)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			if len(pages) == 0 {
				rec.add(ToolStep{Tool: "list_pages", Output: "(no pages yet)"})
				return fantasy.NewTextResponse("(no pages yet)"), nil
			}
			var b strings.Builder
			for _, p := range pages {
				fmt.Fprintf(&b, "- %s (%s)\n", p.Title, p.PageType)
			}
			out := strings.TrimRight(b.String(), "\n")
			rec.add(ToolStep{Tool: "list_pages", Output: out})
			return fantasy.NewTextResponse(out), nil
		},
	)
}

type getPageInput struct {
	Title string `json:"title" description:"The title of the page to read."`
}

func (r *Runner) getPageTool(rec *recorder) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		"get_page",
		"Read a page's source document (markdown with ```sql blocks and {directive:} tags) by its title.",
		func(ctx context.Context, in getPageInput, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			content, err := r.deps.GetPage(ctx, in.Title)
			if err != nil {
				rec.add(ToolStep{Tool: "get_page", Input: in.Title, Output: "error: " + err.Error()})
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			rec.add(ToolStep{Tool: "get_page", Input: in.Title, Output: fmt.Sprintf("%d char(s)", len(content))})
			return fantasy.NewTextResponse(content), nil
		},
	)
}

type createPageInput struct {
	Title   string `json:"title" description:"Human title for the page."`
	Content string `json:"content" description:"The page document: markdown plus named sql fenced blocks (open with three backticks then 'sql block_name') and {directive:} tags. Directive args are COMMA-separated (parsed as YAML — use commas, not spaces): {table: query=block_name}, {metric: query=block_name, label: Revenue}, {bar_chart: query=block_name, x: col, y: col}, {line_chart: query=block_name, x: col, y: col}, {value: query=block_name}."`
}

func (r *Runner) createPageTool(rec *recorder) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		"create_page",
		"Author a new interactive page — a markdown document with embedded ```sql blocks and {directive:} tags (tables, charts, metrics). Use this for any \"page\", \"dashboard\", \"screen\", or interactive view; do NOT use create_report for these.",
		func(ctx context.Context, in createPageInput, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			inputJSON, _ := json.Marshal(in)
			if err := r.deps.CreatePage(ctx, PageDef(in)); err != nil {
				rec.add(ToolStep{Tool: "create_page", Input: string(inputJSON), Output: "error: " + err.Error()})
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			out := fmt.Sprintf("created page %q", in.Title)
			rec.add(ToolStep{Tool: "create_page", Input: string(inputJSON), Output: out})
			return fantasy.NewTextResponse(out), nil
		},
	)
}

type updatePageInput struct {
	Title   string `json:"title" description:"The title of the existing page to rewrite."`
	Content string `json:"content" description:"The full new page document (same format as create_page). Replaces the existing content."`
}

func (r *Runner) updatePageTool(rec *recorder) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		"update_page",
		"Replace an existing page's content. Read it first with get_page so you preserve the parts you are not changing.",
		func(ctx context.Context, in updatePageInput, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			inputJSON, _ := json.Marshal(in)
			if err := r.deps.UpdatePage(ctx, in.Title, in.Content); err != nil {
				rec.add(ToolStep{Tool: "update_page", Input: string(inputJSON), Output: "error: " + err.Error()})
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			out := fmt.Sprintf("updated page %q", in.Title)
			rec.add(ToolStep{Tool: "update_page", Input: string(inputJSON), Output: out})
			return fantasy.NewTextResponse(out), nil
		},
	)
}

type addRelationshipInput struct {
	ChildTable   string `json:"child_table" description:"The table holding the foreign key."`
	ChildColumn  string `json:"child_column" description:"The FK column on the child table."`
	ParentTable  string `json:"parent_table" description:"The referenced (parent) table."`
	ParentColumn string `json:"parent_column" description:"The referenced column on the parent (usually its primary key)."`
	OnDelete     string `json:"on_delete,omitempty" description:"Optional ON DELETE behavior: CASCADE, SET NULL, or RESTRICT."`
}

func (r *Runner) addRelationshipTool(rec *recorder) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		"add_relationship",
		"Declare a foreign-key relationship between two tables (child.column references parent.column).",
		func(ctx context.Context, in addRelationshipInput, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			inputJSON, _ := json.Marshal(in)
			def := RelationshipDef(in)
			if err := r.deps.AddRelationship(ctx, def); err != nil {
				rec.add(ToolStep{Tool: "add_relationship", Input: string(inputJSON), Output: "error: " + err.Error()})
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
			out := fmt.Sprintf("linked %s.%s -> %s.%s", in.ChildTable, in.ChildColumn, in.ParentTable, in.ParentColumn)
			rec.add(ToolStep{Tool: "add_relationship", Input: string(inputJSON), Output: out})
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
