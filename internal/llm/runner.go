// Package llm wires Vibe's in-app conversational agent to an OpenAI-compatible
// LLM endpoint (e.g. a local llama-server at http://localhost:8080/v1) using the
// charmbracelet/fantasy agent framework.
//
// This is the real "wizard layer" agent loop: the model is given the exact set
// of tools defined in internal/agents (the same registry the MCP server
// exposes to external agents), and fantasy drives the multi-step tool-calling
// loop until the model produces a final answer. Every tool call is dispatched
// through an agents.Handler, so sandbox gating and the approval queue apply
// identically whether the call came from Studio chat or an external MCP
// client (#63).
//
// The package depends only on a narrow Handler interface (plus internal/agents
// for the ToolDef/Request/Response types) so it never imports the studio
// package (avoiding an import cycle); studio/app.go supplies the real
// *agents.Handler.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"charm.land/fantasy"

	"github.com/SandwichLabs/vibe/internal/a2ui"
	"github.com/SandwichLabs/vibe/internal/agents"
	"github.com/SandwichLabs/vibe/internal/config"
	"github.com/SandwichLabs/vibe/internal/skills"
)

// Sandbox modes mirror internal/agents' SandboxMode values exactly (they are
// the same gate now — see Handler).
const (
	SandboxReadOnly  = string(agents.ModeReadOnly)
	SandboxReadWrite = string(agents.ModeReadWrite)
	SandboxFull      = string(agents.ModeFull)
)

// maxSteps bounds the tool-calling loop so a confused model can't spin forever.
const maxSteps = 8

// Provider identifiers for the LLM backend.
const (
	ProviderLocal     = "local"     // OpenAI-compatible server (default)
	ProviderAnthropic = "anthropic" // Anthropic Claude
	ProviderOpenAI    = "openai"    // OpenAI (API key, Responses API)
	ProviderGemini    = "gemini"    // Google Gemini
	ProviderOllama    = "ollama"    // Ollama (OpenAI-compatible, local or Ollama Cloud)
	// ProviderCloudflare is Cloudflare AI Gateway's REST API (OpenAI-compatible
	// chat completions, "provider/model" ids).
	ProviderCloudflare = "cloudflare"
)

// Config configures the LLM provider/endpoint and the agent's capability level.
type Config struct {
	Provider string // local | ollama | openai | anthropic | gemini | cloudflare (default local)
	Endpoint string // OpenAI-compatible base URL for the local and ollama providers
	// AccountID and Gateway are the Cloudflare account and optional gateway
	// (sent as cf-aig-gateway-id). Neither is secret.
	AccountID string
	Gateway   string
	Model     string // model id to request; empty = per-provider default
	APIKey    string // bearer token / cloud API key
	Sandbox   string // read-only | read-write | full — gates mutating tools
	// AskBeforeWrites mirrors the file's agents.FilePolicy.AskBeforeWrites
	// (#79): when true and the sandbox isn't read-only, systemPromptFor adds
	// a sentence telling the model every change is shown to the user for
	// approval first, so it doesn't treat a declined/expired tool result as
	// a real failure worth retrying.
	AskBeforeWrites bool
}

// Handler is the narrow request-dispatch surface Runner needs: submit a tool
// call as a JSON-RPC-style request and get back a response. The production
// implementation is *agents.Handler — sandbox gating and the approval queue
// live entirely there, so this package never re-implements permission checks.
type Handler interface {
	Handle(ctx context.Context, req *agents.Request) *agents.Response
}

// ToolStep is a single tool invocation surfaced to the UI so the panel can show
// the agent's tool use ("read schema", "ran SQL", "created table").
type ToolStep struct {
	Tool    string `json:"tool"`
	Input   string `json:"input"`
	Output  string `json:"output"`
	IsError bool   `json:"isError,omitempty"`
}

// ChatResult is the final agent answer plus the tool steps it took.
type ChatResult struct {
	Text  string     `json:"text"`
	Steps []ToolStep `json:"steps"`
}

// Runner builds and runs the fantasy agent for a single chat turn.
type Runner struct {
	handler Handler
	logger  *slog.Logger

	// reasoningRejected/-Mu remember, per (provider,endpoint,model) key (see
	// reasoning.go's reasoningKey), that the endpoint has already rejected
	// the reasoning_effort provider option — set by markReasoningRejected on
	// a retry, consulted by reasoningIsRejected before every later call on
	// this Runner (a subsequent chat turn, and the grounding recheck) so
	// they skip the option outright instead of re-discovering the same
	// rejection.
	reasoningRejectedMu sync.Mutex
	reasoningRejected   map[string]bool

	// openAIBaseURL overrides the OpenAI API base URL. Tests point it at an
	// httptest server; production leaves it empty (api.openai.com).
	openAIBaseURL string
	// cloudflareBaseURL overrides the AI Gateway REST base URL in tests.
	cloudflareBaseURL string
}

// NewRunner constructs a Runner dispatching tool calls through the given
// Handler (in production, an *agents.Handler wired to the open workspace).
func NewRunner(handler Handler, logger *slog.Logger) *Runner {
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{handler: handler, logger: logger}
}

const baseSystemPrompt = `You are the Vibe wizard — the agent layer of Vibe, an agentic data workspace. A workspace is a folder holding notebooks, saved queries and skills as plain files, plus the list of connections (local DuckDB files, MotherDuck, Postgres, SQLite, object storage…) where the user's data actually lives; Vibe queries that data in place through one DuckDB hub. You help the user explore and query their data and build notebooks and dashboards from it.

Prefer reading the schema (introspect_schema, describe_table) before writing SQL or making changes. Keep answers concise and practical. When you create or change schema, briefly confirm what you did.`

// systemPromptFor builds a capability-aware system prompt so the model knows
// exactly what it can do given the active sandbox. It lists every tool in the
// shared agents registry (the same list a MCP client sees) and flags the ones
// the current sandbox will refuse, so the model doesn't wrongly claim to be
// read-only when it has real write tools, nor try a tool that will bounce.
func systemPromptFor(sandbox string, ask bool, skillsBlock string) string {
	var b strings.Builder
	b.WriteString(baseSystemPrompt)

	sc := &agents.SandboxConfig{Mode: agents.SandboxMode(sandbox)}

	b.WriteString("\n\nYour available tools:\n")
	for _, td := range agents.ListToolsForMode(agents.ModeChat) {
		fmt.Fprintf(&b, "- %s: %s", td.Name, td.Description)
		if sc.Check(td.Name) != nil {
			b.WriteString(" [NOT PERMITTED in your current sandbox — calling this will be denied]")
		}
		b.WriteString("\n")
	}

	// Three-tier capability framing (review fix for #63: the old binary
	// writeCapable/else conflated "full" with "any write access at all" and
	// told a read-write agent it had "FULL WRITE ACCESS" to schema/content it
	// cannot actually touch). create_table stands in for the "schema"
	// category gate; create_record stands in for the "data" and "content"
	// gates, which read-write and full both allow (#111).
	fullAccess := sc.Check("create_table") == nil
	dataWrite := sc.Check("create_record") == nil
	switch {
	case fullAccess:
		b.WriteString("\nYOU HAVE FULL WRITE ACCESS to this database — you can build whole workspaces: tables, queries, notebooks, and skills. When the user asks you to build or create something, DO IT by calling the matching tool directly — do NOT say you are read-only and do NOT ask the user to run SQL themselves. Never try to run CREATE/INSERT/UPDATE/DELETE through introspect tools; use create_table for schema changes.\n")
	case dataWrite:
		b.WriteString("\nYou currently have READ-WRITE access to DATA AND CONTENT: you can run_query freely — including INSERT/UPDATE/DELETE — use create_record/update_record/delete_record/bulk_insert to add, change, or remove rows, and write up what you find with add_query_cell, create_dashboard and create_or_update_skill. You CANNOT change the schema — create_table, add_column and drop_table will be denied if you call them. You CAN still propose_schema — it renders a draft inline in chat and never modifies anything by itself, but you cannot follow up with create_table in this sandbox; tell the user so and offer the plan/SQL, or tell them to switch the sandbox to full.\n")
	default:
		b.WriteString("\nYou currently have READ-ONLY access: you can inspect the schema and run read-only queries (SELECT, WITH…SELECT, SHOW, DESCRIBE, EXPLAIN, SUMMARIZE via run_query) but cannot write or modify anything — INSERT/UPDATE/DELETE and every schema/content tool will be denied. You CAN still propose_schema — it renders a draft inline in chat and never modifies anything by itself, but you cannot follow up with create_table in this sandbox. If the user asks to change data or the schema, explain that they can enable more access in the agent settings, and offer the SQL they could run themselves.\n")
	}
	if dataWrite {
		b.WriteString("\nA NOTEBOOK is a JSON document of sql/markdown/a2ui cells; there are no directives or page markup anymore. Use add_query_cell to append a single read-only SQL statement as a new cell (it auto-fits a table/bar/line/stat view the first time it runs). Use create_dashboard to append a full A2UI page (multiple components in one cell) — see the <a2ui> block below for the component schema. Use these for ANY request for a \"page\", \"dashboard\", \"screen\", \"overview\", \"notebook\", or interactive view; there is no create_page tool.\n")
	}
	if ask && sc.Mode != agents.ModeReadOnly {
		b.WriteString("\nEvery change you make (rows, tables, saved queries, notebooks, skills) is shown to the user for approval before it runs. Call the tool normally. If it comes back declined, nothing changed: say so, and don't retry unless the user asks again.\n")
	}
	b.WriteString("\nCONNECTIONS AND FILES — data lives in connections (call list_connections: name, type, status, read_only, local or remote). Name tables as connection.schema.table (introspect_schema takes a connection). You may ATTACH, SET, INSTALL, LOAD, run postgres_query/postgres_execute or read secrets or settings in NO sandbox; do not try. Read a file only with a literal absolute path inside the workspace data/ folder (list_connections returns its path), e.g. read_csv('/path/to/workspace/data/sales.csv'); a path built with ||, a variable or a list is refused, as is any other folder or URL (a URL is allowed only inside a storage connection's scope). A write needs the sandbox to allow it, the connection to be writable (not read_only), and the user's approval, which is always asked for a connection that is not a local file. A refusal tells you why: read it and change the query instead of retrying it.\n")
	b.WriteString("\nSCHEMA DESIGN — when the user describes their business or asks you to design or sketch a database, call propose_schema with the COMPLETE draft: every table with its columns, plus the relationships linking them. Use the friendly field types: ID (the primary key — start every table with one), Text, Long text, Number, Whole number, Money, Date, Time, Yes/No, Link (a reference to another table's ID — pair it with an entry in relationships), Pick (one of a small set of choices). propose_schema renders the draft inline in chat as a readable table-by-table summary (there is no diagram screen) — after it returns, briefly describe what you proposed and ask the user to confirm (\"Want me to build this?\"). When the user asks for changes, re-call propose_schema with the full updated draft; ask at most one short clarifying question after proposing. Only once the user clearly confirms (\"yes\", \"build it\", \"looks good\", ...) do you create it: call create_table once per table, PARENTS BEFORE CHILDREN (a table referenced by a Link must be created before the table that links to it), with every Link declared as an inline foreign key in the child column's type string, e.g. a Link column becomes {\"name\": \"customer_id\", \"type\": \"INTEGER REFERENCES customers(id)\"}. Do not call create_table before the user confirms, and do not silently skip confirmation even if the request sounded like an instruction to build immediately — always show the draft first.\n")

	// present_data / the A2UI component schema (#67). This is chat-mode only
	// — a future task-mode loop (notebook cell authoring, #66) has no
	// present_data tool to call, and describing a component schema the model
	// has no tool to use would only spend context and invite it to try
	// anyway (mirrors schoolwork-workspace's buildSystemPrompt vs
	// buildTaskSystemPrompt split). systemPromptFor is chat-only today, so
	// this always applies; a task-mode prompt builder must not call it.
	b.WriteString("\n\n")
	b.WriteString(a2ui.PromptBlock())

	// Skills (#62) — domain context stored in the file (built-ins plus any
	// user/import skills) that the model can pull in on demand via
	// read_skill. Empty when nothing is chat-visible (fresh workspace).
	b.WriteString(skillsBlock)
	return b.String()
}

// skillsBlockFor fetches the <skills> system-prompt block by calling the
// shared list_skills tool through the handler — the same tool surface
// read_skill is served from, so the block the model sees always matches
// what read_skill can actually return for this file. list_skills is in the
// "introspect" category, so it is never denied regardless of sandbox mode.
// A failure here (no container open, an unexpected result shape) degrades to
// an empty block rather than failing the whole chat turn — a missing
// <skills> section is a much smaller problem than a broken agent turn.
//
// Rendering itself is NOT done here: this only extracts (name, description)
// pairs from the tool result and hands them to skills.RenderSkillsBlock,
// the ONE function that knows how to escape/filter/frame that text for the
// prompt. An earlier version of this function hand-rolled its own
// `<skill name=%q>...` formatting with no escaping or name validation — a
// second, drifted copy of RenderSkillsBlock's logic — which was exactly the
// gap a malicious stored skill row used to inject fake prompt structure
// (closing `</skill>` early and appending a fake "SYSTEM:" line). Never
// reintroduce a second block-formatter here.
func (r *Runner) skillsBlockFor(ctx context.Context) string {
	resp := r.handler.Handle(ctx, &agents.Request{
		JSONRPC: "2.0",
		ID:      "system-prompt-skills",
		Method:  "list_skills",
		Params:  map[string]any{},
	})
	if resp == nil || resp.Error != nil {
		return ""
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		return ""
	}
	raw, ok := result["skills"].([]map[string]any)
	if !ok || len(raw) == 0 {
		return ""
	}
	refs := make([]skills.SkillRef, 0, len(raw))
	for _, s := range raw {
		name, _ := s["name"].(string)
		desc, _ := s["description"].(string)
		refs = append(refs, skills.SkillRef{Name: name, Description: desc})
	}
	return skills.RenderSkillsBlock(refs)
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
	Kind    string `json:"kind"` // "delta" | "tool" | "tool_done" | "final" | "error"
	Text    string `json:"text,omitempty"`
	ID      string `json:"id,omitempty"`   // tool call id, to correlate tool/tool_done
	Tool    string `json:"tool,omitempty"` // tool name
	Input   string `json:"input,omitempty"`
	Output  string `json:"output,omitempty"`
	IsError bool   `json:"isError,omitempty"` // tool_done only: the tool call was denied or failed (Output carries the error message)
}

// Emitter receives StreamEvents as the agent works.
type Emitter func(StreamEvent)

// buildAgent constructs the fantasy agent for a turn: builds the configured
// provider's model and equips it with every tool in the shared agents
// registry. The recorder accumulates tool steps for the final ChatResult.
// withReasoning selects whether the per-mode reasoning-effort provider
// option (#70, reasoning.go) is attached — callers retry once with
// withReasoning=false when the first attempt looks like the local endpoint
// rejecting the param (see mightBeParamRejection).
func (r *Runner) buildAgent(ctx context.Context, cfg Config, rec *recorder, withReasoning bool) (fantasy.Agent, error) {
	languageModel, err := r.languageModel(ctx, cfg)
	if err != nil {
		return nil, err
	}
	opts := []fantasy.AgentOption{
		fantasy.WithSystemPrompt(systemPromptFor(cfg.Sandbox, cfg.AskBeforeWrites, r.skillsBlockFor(ctx))),
		fantasy.WithTools(r.buildTools(rec)...),
		fantasy.WithStopConditions(fantasy.StepCountIs(maxSteps)),
	}
	if withReasoning {
		opts = append(opts, reasoningEffortAgentOptions(cfg)...)
	}
	return fantasy.NewAgent(languageModel, opts...), nil
}

// languageModel resolves the configured provider into a fantasy LanguageModel.
// local -> OpenAI-compatible endpoint; anthropic -> Claude; openai -> OpenAI
// Responses API; gemini -> Google.
func (r *Runner) languageModel(ctx context.Context, cfg Config) (fantasy.LanguageModel, error) {
	provider := strings.TrimSpace(cfg.Provider)
	if provider == "" {
		provider = ProviderLocal
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = config.DefaultModelFor(provider)
	}
	if model == "" {
		return nil, fmt.Errorf("%s has no default model: pick a model in Settings → App", provider)
	}

	p, err := r.newProvider(provider, cfg)
	if err != nil {
		return nil, err
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
	prompt := withPinned(message, pinnedContext)
	promptMessages := buildMessages(history)
	promptMessages = append(promptMessages, fantasy.NewUserMessage(prompt))

	// emittedAny tracks whether ANYTHING has reached the UI via emit during
	// the current streamOnce attempt — review fix: the reasoning-effort
	// retry must never fire once the model has started answering (a delta,
	// a tool chip, anything), even if zero tool steps were recorded, because
	// the UI has already shown the user part of a turn that a retry would
	// then silently restart from scratch.
	var emittedAny bool
	wrap := func(ev StreamEvent) {
		emittedAny = true
		emit(ev)
	}

	streamOnce := func(withReasoning bool) (*fantasy.AgentResult, string, error) {
		agent, err := r.buildAgent(ctx, cfg, rec, withReasoning)
		if err != nil {
			return nil, "", err
		}
		var streamed strings.Builder
		result, err := agent.Stream(ctx, fantasy.AgentStreamCall{
			Messages: promptMessages,
			OnTextDelta: func(_ string, text string) error {
				streamed.WriteString(text)
				wrap(StreamEvent{Kind: "delta", Text: text})
				return nil
			},
			OnToolCall: func(tc fantasy.ToolCallContent) error {
				wrap(StreamEvent{Kind: "tool", ID: tc.ToolCallID, Tool: tc.ToolName, Input: tc.Input})
				return nil
			},
			OnToolResult: func(res fantasy.ToolResultContent) error {
				out, isErr := toolResultOutcome(res.Result)
				wrap(StreamEvent{Kind: "tool_done", ID: res.ToolCallID, Tool: res.ToolName, Output: capForUI(out), IsError: isErr})
				return nil
			},
			// Deliberately NOT forwarded to emit/wrap: fantasy's agent.go
			// calls OnError exactly once, only when Stream is about to
			// return that SAME error terminally (see agent.go: "if err !=
			// nil { opts.OnError(err); return nil, err }") — there is no
			// case where OnError fires and Stream still succeeds. Emitting
			// here would therefore (a) duplicate the single "error"
			// StreamEvent ChatStream already emits below once retries are
			// exhausted, and (b) — the actual bug this fixes — mark
			// emittedAny true for the FIRST attempt's own terminal error,
			// permanently blocking the reasoning-effort retry even though
			// nothing was ever shown to the user (a bare rejection with no
			// prior delta/tool/tool_done is exactly the case that SHOULD
			// retry silently).
			OnError: func(error) {},
		})
		return result, streamed.String(), err
	}

	wantReasoning := !r.reasoningIsRejected(cfg)
	result, streamed, err := streamOnce(wantReasoning)
	if err != nil && wantReasoning && mightBeParamRejection(err) && len(rec.steps) == 0 && !emittedAny {
		// One-time retry without the reasoning-effort param (#70 review fix)
		// — same conditions as Chat's retry, plus: nothing was emitted to the
		// UI yet this attempt.
		r.markReasoningRejected(cfg)
		rec = &recorder{}
		result, streamed, err = streamOnce(false)
	}
	if err != nil {
		if ctx.Err() != nil {
			// The person (or a workspace close) cancelled the turn: that is an
			// end state, not a model failure.
			emit(StreamEvent{Kind: "cancelled"})
			return nil, fmt.Errorf("agent turn cancelled: %w", ctx.Err())
		}
		emit(StreamEvent{Kind: "error", Text: scrubKey(err, cfg.APIKey).Error()})
		return nil, scrubKey(fmt.Errorf("agent error: %w", err), cfg.APIKey)
	}

	text := result.Response.Content.Text()
	if strings.TrimSpace(text) == "" {
		text = streamed
	}

	convo := conversationMessages(promptMessages, result)
	groundedText, groundSteps, groundErr := r.maybeGroundText(ctx, cfg, rec.steps, convo, text, emit)
	if groundErr != nil {
		r.logger.Warn("grounding recheck failed, returning ungrounded answer", "error", scrubKey(groundErr, cfg.APIKey))
	}

	emit(StreamEvent{Kind: "final", Text: groundedText})
	return &ChatResult{Text: groundedText, Steps: appendSteps(rec.steps, groundSteps)}, nil
}

// appendSteps concatenates a turn's own tool steps with any additional
// steps the grounding recheck ran, without mutating either input slice.
func appendSteps(steps, extra []ToolStep) []ToolStep {
	if len(extra) == 0 {
		return steps
	}
	out := make([]ToolStep, 0, len(steps)+len(extra))
	out = append(out, steps...)
	out = append(out, extra...)
	return out
}

// conversationMessages reconstructs the full message sequence a turn
// actually produced: the messages that were sent (history + this turn's
// prompt), followed by every step's own messages in order — which fantasy
// records as assistant tool-call content immediately followed by the tool
// results, ending with the final assistant text message (see fantasy's
// agent.go: StepResult.Messages is that step's slice of the conversation,
// and AgentResult.Steps is those slices in execution order). Concatenating
// promptMessages with every step's Messages therefore reproduces the exact
// conversation the model saw, tool calls and results included — this is
// what maybeGroundText continues instead of starting a fresh, context-free
// exchange (review fix: the original recheck implementation dropped history
// and this turn's own tool calls/results entirely).
func conversationMessages(promptMessages []fantasy.Message, result *fantasy.AgentResult) []fantasy.Message {
	if result == nil {
		return promptMessages
	}
	all := make([]fantasy.Message, 0, len(promptMessages)+len(result.Steps)*2)
	all = append(all, promptMessages...)
	for _, step := range result.Steps {
		all = append(all, step.Messages...)
	}
	return all
}

// capForUI truncates a tool-result string for the UI event stream (#70) —
// independent of and in addition to the RowResultBudgetChars cap already
// applied at the agents.Handler layer for run_query specifically: this cap
// applies to every tool's output, keeping a single Wails/stream event under
// UIEventPayloadBudgetBytes regardless of which tool produced it.
func capForUI(s string) string {
	if len(s) <= agents.UIEventPayloadBudgetBytes {
		return s
	}
	return agents.SafeTruncate(s, agents.UIEventPayloadBudgetBytes) + "…[clipped for display]"
}

// maybeGroundText implements the #70 grounding check for chat mode: if text
// asserts a factual quantity and no grounding tool ran this turn, text is
// sent back once for verification before being returned to the caller.
// Never loops more than once (this function performs exactly one recheck
// call, regardless of what the recheck itself asserts).
//
// Skips entirely when: nothing was asserted; a grounding tool already ran
// (usedGroundingTool); a WRITE tool already ran this turn (wroteThisTurn —
// review fix: never give the model a second tool-calling pass after a write
// already happened, even with the nudge telling it not to repeat actions,
// since the safest fix is to not offer the chance at all); or the file has
// no tables at all, to avoid a pointless loop on an empty workspace.
//
// convo is the exact conversation this turn produced (history, the prompt,
// and every tool call/result — see conversationMessages), which the recheck
// CONTINUES by appending the grounding nudge as the next user message
// (review fix: the original implementation started a fresh two-message
// exchange with no history and no visibility into what the turn's own tools
// had already found).
//
// emit may be nil (Chat has no event stream); when non-nil, a "verifying"
// StreamEvent precedes the recheck call, and the recheck itself runs through
// Stream with the SAME emit callbacks Chat/ChatStream's own turn uses, so
// any tool calls the recheck makes produce live tool chips in the UI exactly
// like the turn's own tool calls (review fix: the recheck previously ran
// through Generate unconditionally, so its tool calls were invisible to the
// UI even in ChatStream). Returns the tool steps the recheck itself
// recorded (empty when emit is nil skips nothing — steps are recorded by
// handlerTool.Run regardless of Generate vs Stream) so Chat/ChatStream can
// fold them into the turn's Steps.
func (r *Runner) maybeGroundText(ctx context.Context, cfg Config, steps []ToolStep, convo []fantasy.Message, text string, emit Emitter) (string, []ToolStep, error) {
	if !assertsFact(text) {
		return text, nil, nil
	}
	if usedGroundingTool(steps) {
		return text, nil, nil
	}
	if wroteThisTurn(steps) {
		return text, nil, nil
	}
	hasTables, err := r.hasAnyTables(ctx)
	if err != nil {
		// Uncertain: err toward checking rather than silently skipping —
		// a false negative here puts an unchecked number in front of the
		// user, a false positive costs one harmless extra turn.
		hasTables = true
	}
	if !hasTables {
		return text, nil, nil
	}

	if emit != nil {
		emit(StreamEvent{Kind: "verifying"})
	}

	groundRec := &recorder{}
	messages := append(append([]fantasy.Message{}, convo...), fantasy.NewUserMessage(groundingPrompt))

	// runGround executes the recheck once, either via Stream (emit != nil,
	// so its own OnToolCall/OnToolResult/OnTextDelta feed the SAME emit the
	// caller's turn used) or via Generate (emit == nil, Chat's case — no live
	// events needed, but groundRec.steps still gets populated since
	// handlerTool.Run records regardless of which path invoked it). Reports
	// whether anything reached emit this attempt, for the same
	// never-retry-after-emitting rule ChatStream's own retry uses.
	runGround := func(withReasoning bool) (result *fantasy.AgentResult, streamedText string, emittedAny bool, err error) {
		agent, err := r.buildAgent(ctx, cfg, groundRec, withReasoning)
		if err != nil {
			return nil, "", false, err
		}
		if emit == nil {
			result, err = agent.Generate(ctx, fantasy.AgentCall{Messages: messages})
			return result, "", false, err
		}
		var streamed strings.Builder
		wrap := func(ev StreamEvent) {
			emittedAny = true
			emit(ev)
		}
		result, err = agent.Stream(ctx, fantasy.AgentStreamCall{
			Messages: messages,
			OnTextDelta: func(_ string, t string) error {
				streamed.WriteString(t)
				wrap(StreamEvent{Kind: "delta", Text: t})
				return nil
			},
			OnToolCall: func(tc fantasy.ToolCallContent) error {
				wrap(StreamEvent{Kind: "tool", ID: tc.ToolCallID, Tool: tc.ToolName, Input: tc.Input})
				return nil
			},
			OnToolResult: func(res fantasy.ToolResultContent) error {
				out, isErr := toolResultOutcome(res.Result)
				wrap(StreamEvent{Kind: "tool_done", ID: res.ToolCallID, Tool: res.ToolName, Output: capForUI(out), IsError: isErr})
				return nil
			},
			// Not forwarded to emit — see ChatStream's streamOnce OnError
			// for why: it always pairs with Stream's own returned error, so
			// forwarding it here would both duplicate reporting and wrongly
			// block this recheck's own reasoning-effort retry.
			OnError: func(error) {},
		})
		return result, streamed.String(), emittedAny, err
	}

	wantReasoning := !r.reasoningIsRejected(cfg)
	result, streamedText, emittedAny, err := runGround(wantReasoning)
	if err != nil && wantReasoning && mightBeParamRejection(err) && len(groundRec.steps) == 0 && !emittedAny {
		// Same reasoning-effort retry rule as the turn's own call (#70
		// review fix #3: the recheck must not silently ignore a rejection —
		// it now shares the exact same Runner-level "rejected" memory).
		r.markReasoningRejected(cfg)
		groundRec.steps = nil
		result, streamedText, _, err = runGround(false)
	}
	if err != nil {
		return text, groundRec.steps, fmt.Errorf("grounding recheck failed: %w", err)
	}

	newText := result.Response.Content.Text()
	if strings.TrimSpace(newText) == "" {
		newText = streamedText
	}
	if strings.TrimSpace(newText) == "" {
		return text, groundRec.steps, nil
	}
	return newText, groundRec.steps, nil
}

// hasAnyTables reports whether the open file has at least one user table,
// via the shared introspect_schema tool (never a bespoke query — one tool
// surface, per CLAUDE.md). Returns an error (rather than false) when the
// check itself fails, so callers can choose to err toward still checking.
func (r *Runner) hasAnyTables(ctx context.Context) (bool, error) {
	resp := r.handler.Handle(ctx, &agents.Request{
		JSONRPC: "2.0", ID: "grounding-check", Method: "introspect_schema", Params: map[string]any{},
	})
	if resp == nil {
		return false, fmt.Errorf("no response from introspect_schema")
	}
	if resp.Error != nil {
		return false, fmt.Errorf("introspect_schema: %s", resp.Error.Message)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		return false, fmt.Errorf("unexpected introspect_schema result shape")
	}
	switch tables := result["tables"].(type) {
	case []string:
		return len(tables) > 0, nil
	case []any:
		return len(tables) > 0, nil
	default:
		return false, nil
	}
}

// toolResultOutcome extracts the text and error state fantasy hands back for
// one finished tool call. A denied/failed tool call surfaces as
// ToolResultOutputContentError, not ...Text (see fantasy's agent.go: an
// IsError ToolResponse is rewrapped into the Error variant before
// OnToolResult ever sees it). Only checking for the Text variant — the
// original bug — left Output empty with no error flag, so the panel rendered
// a plain success chip for a call that was actually denied (review fix #9).
// Extracted as a pure function so it's unit-testable without a real
// streaming HTTP round trip.
func toolResultOutcome(result fantasy.ToolResultOutputContent) (output string, isError bool) {
	if t, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentText](result); ok {
		return t.Text, false
	}
	if e, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentError](result); ok {
		if e.Error != nil {
			return e.Error.Error(), true
		}
		return "", true
	}
	return "", false
}

// buildTools exposes every tool in the shared agents registry to the model,
// unconditionally — the same set an MCP client sees via agents.ListTools() in
// internal/mcp/server.go. Sandbox gating is enforced entirely inside
// Handler.Handle (agents.Handler in production), not by hiding tools here, so
// a denial looks identical whether it came from Studio chat or MCP.
func (r *Runner) buildTools(rec *recorder) []fantasy.AgentTool {
	defs := agents.ListToolsForMode(agents.ModeChat)
	tools := make([]fantasy.AgentTool, 0, len(defs))
	for _, td := range defs {
		tools = append(tools, &handlerTool{td: td, handler: r.handler, rec: rec})
	}
	return tools
}

// handlerTool adapts one agents.ToolDef into a fantasy.AgentTool: its JSON
// schema is generated from ToolDef.Params via agents.ParamsToJSONSchema — the
// same shared helper internal/mcp/server.go's toolDefToMCPTool calls — and
// every call is dispatched through the shared Handler as a JSON-RPC-style
// request, exactly like the MCP bridge does.
type handlerTool struct {
	td      agents.ToolDef
	handler Handler
	rec     *recorder
	opts    fantasy.ProviderOptions
}

func (t *handlerTool) Info() fantasy.ToolInfo {
	return ToolInfo(t.td)
}

// ToolInfo returns the fantasy.ToolInfo (JSON schema) the runner advertises
// for a given tool definition. Exported so other packages' tests (see
// internal/mcp's TestToolSchemas_MatchRunner) can assert this stays
// byte-identical to the MCP bridge's schema for the same ToolDef (#63 review
// fix) without needing access to the unexported handlerTool type.
func ToolInfo(td agents.ToolDef) fantasy.ToolInfo {
	props, required := agents.ParamsToJSONSchema(td.Params)
	return fantasy.ToolInfo{
		Name:        td.Name,
		Description: td.Description,
		Parameters:  props,
		Required:    required,
	}
}

func (t *handlerTool) ProviderOptions() fantasy.ProviderOptions { return t.opts }

func (t *handlerTool) SetProviderOptions(opts fantasy.ProviderOptions) { t.opts = opts }

func (t *handlerTool) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	// A cancelled turn runs no further tool: this is the "between steps" stop.
	if err := ctx.Err(); err != nil {
		return fantasy.ToolResponse{}, err
	}
	params := map[string]any{}
	if strings.TrimSpace(call.Input) != "" {
		if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
			msg := fmt.Sprintf("invalid arguments for %s: %v", t.td.Name, err)
			t.rec.add(ToolStep{Tool: t.td.Name, Input: call.Input, Output: "error: " + msg, IsError: true})
			return fantasy.NewTextErrorResponse(msg), nil
		}
	}

	resp := t.handler.Handle(ctx, &agents.Request{
		JSONRPC: "2.0",
		ID:      call.ID,
		Method:  t.td.Name,
		Params:  params,
	})

	if resp.Error != nil {
		t.rec.add(ToolStep{Tool: t.td.Name, Input: call.Input, Output: "error: " + resp.Error.Message, IsError: true})
		return fantasy.NewTextErrorResponse(resp.Error.Message), nil
	}

	data, err := json.Marshal(resp.Result)
	if err != nil {
		msg := fmt.Sprintf("failed to marshal result: %v", err)
		t.rec.add(ToolStep{Tool: t.td.Name, Input: call.Input, Output: "error: " + msg, IsError: true})
		return fantasy.NewTextErrorResponse(msg), nil
	}
	out := string(data)
	t.rec.add(ToolStep{Tool: t.td.Name, Input: call.Input, Output: out})
	return fantasy.NewTextResponse(out), nil
}
