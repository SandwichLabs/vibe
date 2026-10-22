package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAskTarget_NamesTheProviderAndModel(t *testing.T) {
	assert.Equal(t, "Anthropic (claude-sonnet-5-5)", LLMConfig{Provider: LLMProviderAnthropic}.AskTarget())
	assert.Equal(t, "OpenAI (gpt-x)", LLMConfig{Provider: LLMProviderOpenAI, Model: "gpt-x"}.AskTarget())
}

func TestAskTarget_ALocalServerIsNamedByHostOnly(t *testing.T) {
	got := LLMConfig{Provider: LLMProviderLocal, Endpoint: "http://user:secret@localhost:8080/v1/secret?k=1"}.AskTarget()
	assert.Equal(t, "Local server at http://localhost:8080 (local)", got)
	assert.NotContains(t, got, "secret")
	assert.Contains(t, LLMConfig{Provider: LLMProviderLocal, Endpoint: "::bad"}.AskTarget(), "an endpoint you set")
}
