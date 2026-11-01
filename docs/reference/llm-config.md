# LLM provider configuration

The in-app wizard agent's model provider is **app-global and viper-driven**:
configure it once in Studio's Settings → App (which edits `~/.slaw.yaml`), `slaw config llm`, `~/.slaw.yaml` itself (or via `SLAW_*` environment variables) and
every workspace you open uses it. (The agent *sandbox* — read-only / read-write /
full — and the "Ask before changes" switch (vibe-app#79, on by default: the
agent shows every write for approval before it runs) are per-workspace
settings, since they're a security boundary for that specific workspace; the
workspace's `slaw.yaml` can cap them for everyone. Both live in Studio's
Settings → Workspace → The agent.)

## Providers

Pick one in Studio under Settings, App, "The assistant's model" (**Change provider or model**), or with `slaw config llm set`. Each provider shows its label and a one-line description, then only the fields it uses.

| Provider (picker order) | `llm.provider` | Backend | Credential |
|---|---|---|---|
| Local server (default) | `local` | OpenAI-compatible server at `llm.endpoint` (llama-server, LM Studio) | none (optional) |
| Ollama | `ollama` | Ollama's OpenAI-compatible API, default `http://localhost:11434/v1` | none locally; optional `OLLAMA_API_KEY` for Ollama Cloud |
| OpenAI | `openai` | OpenAI API, Responses API | `OPENAI_API_KEY` |
| Anthropic | `anthropic` | Claude | `ANTHROPIC_API_KEY` |
| Google Gemini | `gemini` | Gemini | `GEMINI_API_KEY` (or `GOOGLE_API_KEY`) |
| Cloudflare AI Gateway | `cloudflare` | Cloudflare AI Gateway REST API | Cloudflare API token, `CLOUDFLARE_API_TOKEN` |

The list lives in one place, `internal/config/providers.go`; Studio, `slaw config llm` and `slaw config llm-key --provider` all read it.

The default is **local**, pointing at `http://localhost:8080/v1`.

### Ollama

`llm.provider: ollama` uses the OpenAI-compatible API (`/v1/chat/completions`) with tool calling. The endpoint defaults to `http://localhost:11434/v1`. Settings lists the models you have installed (from `GET /api/tags`; no key is sent). Ollama has **no default model**: with `llm.model` empty the agent says "pick a model in Settings → App" instead of guessing. Use a model that supports tools (for example `qwen3`).

For **Ollama Cloud**, set the endpoint to `https://ollama.com/v1` and save an API key (`OLLAMA_API_KEY`, or the keychain item `llm-ollama`). Ollama documents that base URL as OpenAI-compatible (checked 2026-10-07); Slaw has not run it against a live Ollama Cloud account. The installed-model list needs a local Ollama, so type the model id for Cloud.

### Cloudflare AI Gateway

`llm.provider: cloudflare` sends the agent through the AI Gateway REST API (the current API, May 2026; the older `gateway.ai.cloudflare.com/.../compat` endpoint is deprecated for this use):

```
POST https://api.cloudflare.com/client/v4/accounts/{account_id}/ai/v1/chat/completions
Authorization: Bearer <Cloudflare API token>
cf-aig-gateway-id: <gateway>        # only when llm.gateway is set
```

- `llm.account_id` (or `SLAW_LLM_ACCOUNT_ID`; falls back to `CLOUDFLARE_ACCOUNT_ID`): 32 hexadecimal characters. **Not secret.** Validated before it goes into the URL.
- `llm.gateway` (optional, **not secret**): a gateway id (letters, digits, `-`, `_`). Blank uses your account's `default` gateway.
- The model is `provider/model`, for example `openai/gpt-5.5`, `anthropic/claude-sonnet-5-5` or a Workers AI model such as `@cf/moonshotai/kimi-k2.6` (Workers AI models need a gateway id). The default is `openai/gpt-5.5`. Third-party models are billed through Cloudflare's Unified Billing, so there is no separate provider key.
- The token is `CLOUDFLARE_API_TOKEN` or the keychain item `llm-cloudflare`, write-only like every other key.

Model names and the default were last checked against the Cloudflare docs on 2026-10-07.

## Config file — `~/.slaw.yaml`

```yaml
llm:
  provider: local          # local | ollama | openai | anthropic | gemini | cloudflare
  endpoint: http://localhost:8080/v1   # local and ollama
  model: ""                # blank = the provider's default (Ollama has none)
  account_id: ""           # cloudflare only; not secret
  gateway: ""              # cloudflare only; blank = the default gateway
  # api_key: ""            # legacy; keep it out of this file, see "API key" below
```

Settings and `slaw config llm set` edit `provider`, `model`, `endpoint`, `account_id` and `gateway` in place: comments and every other key stay, the file is created with mode 0600 if missing, a symlink is followed, and the write is atomic. They never write or change `llm.api_key`. A field set by an environment variable (below) is shown disabled in Settings, with the variable's name; saving leaves the file's own value for it.

Per-provider default models (overridable via `llm.model`; the Settings model field also offers a few suggestions per provider):
- `local` → `local`
- `ollama` → none
- `openai` → `gpt-5.5`
- `anthropic` → `claude-sonnet-5-5` (also `claude-opus-5-5`, `claude-haiku-4-5-20251001`)
- `gemini` → `gemini-3.5-flash`
- `cloudflare` → `openai/gpt-5.5`

These defaults were last checked on 2026-10-07 and age as providers release
models; set `llm.model` (or `SLAW_LLM_MODEL`) to use any other model id. The
`openai` provider sends every model id to the Responses API, so ids Slaw has
never heard of (a new release, a fine-tune) work too. For reasoning models
Slaw asks for medium reasoning effort; if a model rejects that parameter Slaw
retries the turn once without it.

## From a terminal

```bash
slaw config llm show                       # provider, model ("(default)" when unset), endpoint/account/gateway, env overrides
slaw config llm set --provider ollama --model qwen3:0.6b
slaw config llm set --provider cloudflare --account-id <id> [--gateway <id>]
slaw config llm models                     # installed models (local, ollama) or the suggestions
slaw config llm-key set --provider cloudflare   # keys stay on llm-key
```

`set` changes only the flags you pass; `--model ""` goes back to the default.

## Environment variables

Any key maps to `SLAW_` + the upper-snake-cased path, and overrides the file:

```bash
export SLAW_LLM_PROVIDER=anthropic
export SLAW_LLM_MODEL=claude-sonnet-5-5
export ANTHROPIC_API_KEY=sk-ant-...      # cloud key via the standard env var
```

```bash
# OpenAI:
export SLAW_LLM_PROVIDER=openai
export OPENAI_API_KEY=sk-...
```

```bash
# Cloudflare AI Gateway:
export SLAW_LLM_PROVIDER=cloudflare
export SLAW_LLM_ACCOUNT_ID=<32-hex account id>   # or CLOUDFLARE_ACCOUNT_ID
export CLOUDFLARE_API_TOKEN=...
```

```bash
# Local, custom endpoint/model:
export SLAW_LLM_ENDPOINT=http://localhost:8080/v1
export SLAW_LLM_MODEL=my-local-model
```

`SLAW_LLM_PROVIDER`, `SLAW_LLM_MODEL`, `SLAW_LLM_ENDPOINT`, `SLAW_LLM_ACCOUNT_ID` and `SLAW_LLM_GATEWAY` override the file (and disable the matching Settings field). API keys are read from the provider's **standard** env var
(`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `GEMINI_API_KEY`, `OLLAMA_API_KEY`, `CLOUDFLARE_API_TOKEN`), so secrets stay out of config files.
An explicit `SLAW_LLM_API_KEY` takes precedence. See "API key" below.

## API key (vibe-app#114)

The key lives in your **OS keychain** (macOS Keychain, Windows Credential
Manager, Linux Secret Service), never in a config file, a log or the agent's
context. It is write-only: Slaw can save, replace and remove it, and tell you
where it comes from, but nothing shows it back.

**Where a key is read from**, first match wins:

1. `SLAW_LLM_API_KEY`
2. the provider's standard variable (`ANTHROPIC_API_KEY`; `OPENAI_API_KEY`; `GEMINI_API_KEY`, then `GOOGLE_API_KEY`; `OLLAMA_API_KEY`; `CLOUDFLARE_API_TOKEN`)
3. the keychain item for the provider (`llm-anthropic`, `llm-openai`, `llm-gemini`, `llm-local`, `llm-ollama`, `llm-cloudflare`)
4. `llm.api_key` in `~/.slaw.yaml` (legacy, read-only: Slaw never writes a key there). It is a single value, used for whichever provider is selected, so switching provider while only the file key exists sends that key to the new provider. Settings warns before that ("The plaintext key in ~/.slaw.yaml would be sent to Cloudflare…"), asks you to confirm, and offers the keychain move first. The keychain and provider variables are per provider.

`--provider` accepts any provider id in the table above; anything else
is refused before a keychain item is created. A key never appears in an error
message, a streamed agent error or a log line: a provider that echoes it back
in a failure has it replaced with `[redacted]`. (OpenAI's own 401 already shows
only a masked form ending in the key's last four characters; that text is
passed through as is.)

**Set it** in Studio under Settings, App, "The assistant's model" (paste a key,
Save to keychain; then "In your keychain, Replace, Clear"), or from a terminal:

```bash
slaw config llm-key set            # prompts without echo; or pipe one line on stdin
slaw config llm-key status         # where the key comes from (never the key)
slaw config llm-key clear
slaw config llm-key set --provider openai   # or any provider id above
```

**No keychain** (an SSH session, a headless box, a locked collection)? Studio
says so and Slaw does not fall back to a file. Set `SLAW_LLM_API_KEY` in the
environment Slaw starts from instead. Every keychain call has a short timeout,
so a locked keychain shows as "not reachable" instead of freezing the app.

**Migration.** If `~/.slaw.yaml` still has a plaintext `llm.api_key` and the
keychain works, Studio shows "Found a key in ~/.slaw.yaml. Move it to the
keychain" (or run `slaw config llm-key migrate`). Accepting stores the key,
reads it back to be sure, then blanks only that value in the file
(`api_key: ""`): comments and the rest of the file stay as they are. A backup is
written first to `~/.slaw.yaml.bak` (mode 0600) with the key already removed, so
the backup does not keep the secret. If anything fails before the final rename,
the file is untouched. A symlinked `~/.slaw.yaml` is followed: the target is edited in place (its backup sits next to it) and the link stays. If the key is written in a form that cannot be blanked safely (a YAML alias or tag, a multi-line value, several documents, or repeated in a comment) Slaw says so and tells you to run `slaw config llm-key set` and remove it by hand; nothing is stored first. If the keychain already holds a different key, migrating asks first in Studio and needs `--force` on the CLI.

Testing and QA can point at another file with `SLAW_CONFIG_FILE` and another
keychain service name with `SLAW_KEYRING_SERVICE`.

## Precedence

Built-in defaults → `~/.slaw.yaml` → `SLAW_*` environment variables. The API key follows its own order, above.
