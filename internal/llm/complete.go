package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"charm.land/fantasy"
)

// MaxCompletionTokens is the output cap of Complete, whatever a caller asks.
const MaxCompletionTokens = 4000

// ErrNoProvider means no model is set up to answer: a cloud provider with no
// key, or a local one with no endpoint.
var ErrNoProvider = errors.New("no model is configured")

// Configured reports whether cfg names a provider that could answer: a cloud
// provider needs its key, a local or Ollama server needs an endpoint. It does
// not reach the network, so a configured server that is down still fails in
// Complete.
func Configured(cfg Config) bool {
	switch strings.TrimSpace(cfg.Provider) {
	case ProviderLocal, ProviderOllama, "":
		return strings.TrimSpace(cfg.Endpoint) != ""
	case ProviderCloudflare:
		return strings.TrimSpace(cfg.APIKey) != "" && strings.TrimSpace(cfg.AccountID) != ""
	default:
		return strings.TrimSpace(cfg.APIKey) != ""
	}
}

// Complete is a single completion: one system text, one user text, no tools,
// no history. It is not the agent: the request never carries a tool, the tool
// registry and task mode are not reachable from here, and the answer is
// capped at maxTokens (at most MaxCompletionTokens). The error text has the
// key scrubbed from it.
func (r *Runner) Complete(ctx context.Context, cfg Config, system, prompt string, maxTokens int64) (string, error) {
	if !Configured(cfg) {
		return "", ErrNoProvider
	}
	if maxTokens <= 0 || maxTokens > MaxCompletionTokens {
		maxTokens = MaxCompletionTokens
	}
	model, err := r.languageModel(ctx, cfg)
	if err != nil {
		return "", scrubKey(err, cfg.APIKey)
	}
	msgs := fantasy.Prompt{}
	if strings.TrimSpace(system) != "" {
		msgs = append(msgs, fantasy.NewSystemMessage(system))
	}
	msgs = append(msgs, fantasy.NewUserMessage(prompt))
	// Tools is left nil on purpose: a completion has no tools to call.
	resp, err := model.Generate(ctx, fantasy.Call{Prompt: msgs, MaxOutputTokens: &maxTokens})
	if err != nil {
		return "", scrubKey(fmt.Errorf("completion failed: %w", err), cfg.APIKey)
	}
	return resp.Content.Text(), nil
}
