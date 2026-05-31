# LLM provider configuration

The in-app wizard agent's model provider is **app-global and viper-driven**:
configure it once in `~/.vibe.yaml` (or via `VIBE_*` environment variables) and
every `.vibe` you open uses it. (The agent *sandbox* — read-only / read-write /
full — remains a per-`.vibe` setting, since it's a security boundary for that
specific app.)

## Providers

| Provider    | `llm.provider` | Backend                                   | API key |
|-------------|----------------|-------------------------------------------|---------|
| Local (default) | `local`    | OpenAI-compatible server (`llm.endpoint`) | none (any local server) |
| Anthropic   | `anthropic`    | Claude                                    | `ANTHROPIC_API_KEY` |
| Gemini      | `gemini`       | Google Gemini                             | `GEMINI_API_KEY` (or `GOOGLE_API_KEY`) |

The default is **local**, pointing at `http://localhost:8080/v1` — a local
llama-server / LM Studio / Ollama OpenAI shim, etc.

## Config file — `~/.vibe.yaml`

```yaml
llm:
  provider: local                      # local | anthropic | gemini
  endpoint: http://localhost:8080/v1   # local provider only
  model: ""                            # blank = per-provider default
  # api_key: ""                        # optional; prefer the standard env var below
```

Per-provider default models (overridable via `llm.model`):
- `local` → `local`
- `anthropic` → `claude-sonnet-4-5`
- `gemini` → `gemini-2.5-flash`

## Environment variables

Any key maps to `VIBE_` + the upper-snake-cased path, and overrides the file:

```bash
export VIBE_LLM_PROVIDER=anthropic
export VIBE_LLM_MODEL=claude-sonnet-4-5
export ANTHROPIC_API_KEY=sk-ant-...      # cloud key via the standard env var
```

```bash
# Local, custom endpoint/model:
export VIBE_LLM_ENDPOINT=http://localhost:8080/v1
export VIBE_LLM_MODEL=my-local-model
```

Cloud API keys are read from the provider's **standard** env var
(`ANTHROPIC_API_KEY`, `GEMINI_API_KEY`) when `llm.api_key` is unset, so secrets
stay out of config files. An explicit `VIBE_LLM_API_KEY` takes precedence.

## Precedence

Built-in defaults → `~/.vibe.yaml` → `VIBE_*` environment variables.
