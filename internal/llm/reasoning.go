package llm

import (
	"strings"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/openai"
	"charm.land/fantasy/providers/openaicompat"
)

// Per-mode reasoning effort (#70). Written against charm.land/fantasy@v0.28.0
// and rechecked at v0.45.2: the openaicompat provider package exposes a
// ReasoningEffort provider option (providers/openaicompat/provider_options.go,
// forwarded to the request as OpenAI's `reasoning_effort` field by
// language_model_hooks.go). The google provider package has no equivalent.
// Since v0.45 the anthropic package has an `Effort` option, but this Runner
// does not set it, so reasoning effort stays a no-op for anthropic and
// google (wiring anthropic.Effort is a separate change). The openai
// provider (Responses API) takes it as openai.ResponsesProviderOptions; fantasy
// only forwards it for models it recognises as reasoning models, so a
// non-reasoning model id simply ignores it rather than being sent a parameter
// it would reject.

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
// per-mode reasoning effort for the local OpenAI-compatible provider and the
// openai provider, or nil for every other provider (an empty provider is
// treated as ProviderLocal, as languageModel does) — see the doc comment above.
func reasoningEffortAgentOptions(cfg Config) []fantasy.AgentOption {
	provider := strings.TrimSpace(cfg.Provider)
	effort := openai.ReasoningEffortMedium // chat and task are both "medium" (#70)
	if provider == ProviderOpenAI {
		return []fantasy.AgentOption{fantasy.WithProviderOptions(openai.NewResponsesProviderOptions(
			&openai.ResponsesProviderOptions{ReasoningEffort: &effort},
		))}
	}
	if provider != "" && provider != ProviderLocal {
		return nil
	}
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
