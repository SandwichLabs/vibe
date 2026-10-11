package llm

import (
	"context"
	"fmt"
	"strings"

	"charm.land/fantasy"
)

// Test-only helpers: kept out of the production build.

// Chat runs one agent turn (non-streaming). Kept for the back-compat AgentChat
// binding and unit tests. pinnedContext, if non-empty, is prepended to the user
// message so the agent knows what the user is currently looking at.
func (r *Runner) Chat(ctx context.Context, cfg Config, message, pinnedContext string) (*ChatResult, error) {
	if strings.TrimSpace(message) == "" {
		return nil, fmt.Errorf("empty message")
	}
	rec := &recorder{}
	prompt := withPinned(message, pinnedContext)
	promptMessages := []fantasy.Message{fantasy.NewUserMessage(prompt)}

	wantReasoning := !r.reasoningIsRejected(cfg)
	agent, err := r.buildAgent(ctx, cfg, rec, wantReasoning)
	if err != nil {
		return nil, err
	}
	result, err := agent.Generate(ctx, fantasy.AgentCall{Prompt: prompt})
	if err != nil && wantReasoning && mightBeParamRejection(err) && len(rec.steps) == 0 {
		// One-time retry without the reasoning-effort param (#70 review fix):
		// only when the first attempt recorded ZERO tool steps — proof no
		// tool (in particular no write) ran before the error — and the error
		// clearly names the reasoning parameter (mightBeParamRejection is now
		// narrow). Retrying after a tool already ran would re-send the whole
		// prompt and could re-execute a write a second time.
		r.markReasoningRejected(cfg)
		rec = &recorder{}
		if agent2, err2 := r.buildAgent(ctx, cfg, rec, false); err2 == nil {
			result, err = agent2.Generate(ctx, fantasy.AgentCall{Prompt: prompt})
		}
	}
	if err != nil {
		return nil, scrubKey(fmt.Errorf("agent error: %w", err), cfg.APIKey)
	}

	text := result.Response.Content.Text()
	convo := conversationMessages(promptMessages, result)
	groundedText, groundSteps, groundErr := r.maybeGroundText(ctx, cfg, rec.steps, convo, text, nil)
	if groundErr != nil {
		r.logger.Warn("grounding recheck failed, returning ungrounded answer", "error", scrubKey(groundErr, cfg.APIKey))
	}
	return &ChatResult{Text: groundedText, Steps: appendSteps(rec.steps, groundSteps)}, nil
}
