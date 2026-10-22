package config

import (
	"net/url"
	"strings"
)

// AskTarget names, in plain words, where a page's `ask` would send text: the
// provider and the model that would be requested, for the allow prompt. A
// local or Ollama server is named by its host only (never a path, a query or
// credentials in the URL). It reaches no network and reads no key.
func (c LLMConfig) AskTarget() string {
	id := strings.TrimSpace(c.Provider)
	if id == "" {
		id = LLMProviderLocal
	}
	info, ok := LLMProvider(id)
	label := id
	if ok {
		label = info.Label
	}
	model := c.EffectiveModel()
	switch id {
	case LLMProviderLocal, LLMProviderOllama:
		label += " at " + hostOf(firstNonEmpty(c.Endpoint, info.DefaultEndpoint))
	}
	if model != "" {
		return label + " (" + model + ")"
	}
	return label
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// hostOf is scheme://host[:port] of a URL, or "an endpoint you set" when it
// does not parse as one.
func hostOf(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return "an endpoint you set"
	}
	return u.Scheme + "://" + u.Host
}
