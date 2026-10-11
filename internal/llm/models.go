package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/SandwichLabs/vibe/internal/config"
)

// ModelList is the answer to "which models does this server have".
type ModelList struct {
	Models []string `json:"models"`
	// Error is a short human message when the list could not be read.
	Error string `json:"error,omitempty"`
	// Unreachable is true when nothing answered at the address.
	Unreachable bool `json:"unreachable,omitempty"`
}

const (
	modelListTimeout = 5 * time.Second
	modelListMaxBody = 1 << 20
)

// ListModels asks a local OpenAI-compatible server (GET {endpoint}/models) or
// Ollama (GET {host}/api/tags) which models it has. Names only, no key is
// sent, 5 s timeout, 1 MB cap. Other providers have no list (use the static
// suggestions in config).
func ListModels(ctx context.Context, provider, endpoint string) ModelList {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		if p, ok := config.LLMProvider(provider); ok {
			endpoint = p.DefaultEndpoint
		}
	}
	if err := config.ValidateEndpoint(endpoint); err != nil {
		return ModelList{Error: err.Error()}
	}
	var target string
	switch provider {
	case config.LLMProviderLocal:
		target = strings.TrimRight(endpoint, "/") + "/models"
	case config.LLMProviderOllama:
		u, _ := url.Parse(endpoint)
		u.Path = strings.TrimSuffix(strings.TrimRight(u.Path, "/"), "/v1") + "/api/tags"
		u.RawQuery, u.Fragment = "", ""
		target = u.String()
	default:
		return ModelList{Error: "this provider has no model list"}
	}
	ctx, cancel := context.WithTimeout(ctx, modelListTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return ModelList{Error: "invalid endpoint"}
	}
	resp, err := (&http.Client{Timeout: modelListTimeout}).Do(req)
	if err != nil {
		return ModelList{Unreachable: true, Error: "nothing answered at " + endpoint}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return ModelList{Error: fmt.Sprintf("the server answered %s", resp.Status)}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, modelListMaxBody+1))
	if err != nil || len(raw) > modelListMaxBody {
		return ModelList{Error: "the model list was unreadable or too large"}
	}
	names, err := parseModelNames(provider, raw)
	if err != nil {
		return ModelList{Error: "the server's model list was not in a known format"}
	}
	return ModelList{Models: names}
}

func parseModelNames(provider string, raw []byte) ([]string, error) {
	var names []string
	if provider == config.LLMProviderOllama {
		var v struct {
			Models []struct {
				Name         string   `json:"name"`
				Capabilities []string `json:"capabilities"`
			} `json:"models"`
		}
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		for _, m := range v.Models {
			// When the server reports capabilities, keep only models that can
			// call tools (the agent needs them); no field means keep everything.
			if m.Capabilities != nil && !slices.Contains(m.Capabilities, "tools") {
				continue
			}
			names = append(names, m.Name)
		}
	} else {
		var v struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		for _, m := range v.Data {
			names = append(names, m.ID)
		}
	}
	out := names[:0]
	for _, n := range names {
		if n = strings.TrimSpace(n); n != "" && config.ValidateModel(n) == nil {
			out = append(out, n)
		}
	}
	if out == nil {
		return []string{}, nil
	}
	sort.Strings(out)
	if len(out) > 500 {
		out = out[:500]
	}
	return out, nil
}
