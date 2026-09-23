package llm

import (
	"strings"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/openai"
	"charm.land/fantasy/providers/openaicompat"
)

// Per-mode reasoning effort (#70). Checked directly against
// charm.land/fantasy@v0.28.0 (the version pinned in go.mod): only the
// openaicompat provider package exposes a ReasoningEffort provider option
// (providers/openaicompat/provider_options.go, forwarded to the request as
// OpenAI's `reasoning_effort` field by language_model_hooks.go) — the
// anthropic and google provider packages this Runner also supports do not
// have an equivalent option in this fantasy version, so reasoning effort is
// a no-op for those providers, not a gap in this feature.
const (
	reasoningModeChat = "chat"
	reasoningModeTask = "task"
)

// reasoningEffortForMode returns the effort level for a given agent-surface
// mode. Chat and task are both "medium" per #70 — there is no "oneshot"
// mode in this codebase today (only chat and task-mode notebook authoring
// exist); when one is added it should pass reasoningModeChat/Task's sibling
// here with openai.ReasoningEffortLow.
func reasoningEffortForMode(mode string) openai.ReasoningEffort {
	switch mode {
	case reasoningModeTask:
		return openai.ReasoningEffortMedium
	default:
		return openai.ReasoningEffortMedium
	}
}

// reasoningKey identifies the (provider, endpoint, model) combination a
// "the endpoint rejected reasoning_effort" finding applies to — Runner
// remembers rejections per key (see Runner.reasoningRejected) so a later
// call with the SAME cfg (including the grounding recheck, which reuses the
// turn's cfg) skips the option outright instead of re-discovering the same
// rejection and paying for a second failed request.
func reasoningKey(cfg Config) string {
	return cfg.Provider + "|" + cfg.Endpoint + "|" + cfg.Model
}

// reasoningIsRejected reports whether this Runner has already seen the
// endpoint identified by cfg reject the reasoning_effort parameter.
func (r *Runner) reasoningIsRejected(cfg Config) bool {
	r.reasoningRejectedMu.Lock()
	defer r.reasoningRejectedMu.Unlock()
	return r.reasoningRejected[reasoningKey(cfg)]
}

// markReasoningRejected records that cfg's endpoint rejected
// reasoning_effort, for reasoningIsRejected to consult on every later call
// (this turn's retry, a later chat turn, and the grounding recheck) for the
// lifetime of this Runner.
func (r *Runner) markReasoningRejected(cfg Config) {
	r.reasoningRejectedMu.Lock()
	defer r.reasoningRejectedMu.Unlock()
	if r.reasoningRejected == nil {
		r.reasoningRejected = make(map[string]bool, 1)
	}
	r.reasoningRejected[reasoningKey(cfg)] = true
}

// reasoningEffortAgentOptions returns the AgentOptions that set the
// per-mode reasoning effort for the local OpenAI-compatible provider, or
// nil for every other provider (or an empty provider, which languageModel
// treats as ProviderLocal) — see the package doc comment above.
func reasoningEffortAgentOptions(cfg Config, mode string) []fantasy.AgentOption {
	provider := strings.TrimSpace(cfg.Provider)
	if provider != "" && provider != ProviderLocal {
		return nil
	}
	effort := reasoningEffortForMode(mode)
	return []fantasy.AgentOption{fantasy.WithProviderOptions(fantasy.ProviderOptions{
		openaicompat.Name: &openaicompat.ProviderOptions{ReasoningEffort: &effort},
	})}
}

// mightBeParamRejection reports whether err looks like a local
// OpenAI-compatible server rejecting the reasoning_effort parameter
// specifically (some llama.cpp-server-style backends 400 on an unknown JSON
// field instead of silently ignoring it) rather than some other failure.
//
// Deliberately narrow (review fix, round 2): an earlier version also matched
// generic phrases like "unrecognized"/"unknown parameter"/"invalid
// parameter" — those also match errors that have nothing to do with
// reasoning_effort (a malformed tool-call argument, an unrelated 400 from
// the endpoint), which caused the retry-without-reasoning path to fire after
// a request that had ALREADY executed a write tool, re-sending the whole
// turn and re-running that tool a second time. Only text that clearly names
// the reasoning parameter — "reasoning_effort" or the bare word "reasoning"
// — counts; every other error is treated as a genuine failure to surface,
// never as grounds for a retry.
func mightBeParamRejection(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "reasoning_effort") || strings.Contains(msg, "reasoning")
}
