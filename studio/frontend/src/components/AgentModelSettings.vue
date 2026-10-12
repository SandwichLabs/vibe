<script setup lang="ts">
// Settings > App > The agent's model. App-global (every workspace): the
// provider picker, model, endpoint and Cloudflare account are saved to
// ~/.vibe.yaml (never the key; docs/reference/llm-config.md). A field set by a
// VIBE_LLM_* variable is shown disabled with the variable's name.
// The API key is write-only (SPEC 09, #114): it is saved to the OS keychain and
// never shown again. "•••• In your keychain · Replace · Clear". Nothing here can
// read a key back; the bindings only set, clear and report where it comes from.
// The state and actions are useLlmSettings.
import Icon from './Icon.vue'
import { useLlmSettings } from '../composables/useLlmSettings'

const {
  view,
  keyStatus,
  editing,
  changing,
  form,
  draft,
  busy,
  message,
  settingsMsg,
  settingsErr,
  keyInput,
  models,
  providers,
  savedInfo,
  formInfo,
  keyProviderArg,
  keyInfo,
  env,
  source,
  showForm,
  keyLabel,
  keyOptionalNote,
  keyPending,
  fileKeyHazard,
  needsModel,
  effectiveShown,
  beginChange,
  cancelChange,
  pickProvider,
  onProviderKey,
  listsModels,
  maybeLoadModels,
  loadModels,
  suggestions,
  modelPlaceholder,
  ollamaSelect,
  ollamaOptions,
  accountError,
  gatewayError,
  needsAccount,
  saveSettings,
  startReplace,
  save,
  clearKey,
  migrate,
} = useLlmSettings()
</script>

<template>
  <div data-testid="agent-model-settings">
    <div class="sec-title"><h2 style="font-size: 22px;">The agent's model</h2><span class="label">Every workspace</span></div>
    <div class="card card-pad col" style="gap: var(--sh-space-4);">
      <!-- Read-only: what the agent will actually use -->
      <div v-if="view && !changing" class="col" style="gap: var(--sh-space-2);" data-testid="llm-summary">
        <div class="kv"><span class="k">Provider</span><span class="v">{{ savedInfo?.label ?? view.provider }}<span v-if="env.provider" class="envtag" data-testid="llm-env-provider"> &middot; set by {{ env.provider }}</span></span></div>
        <div class="kv">
          <span class="k">Model</span>
          <span class="v" data-testid="llm-effective-model">
            <template v-if="effectiveShown">{{ effectiveShown }}</template>
            <template v-else>None yet: pick a model below</template>
            <span v-if="env.model" class="envtag"> &middot; set by {{ env.model }}</span>
          </span>
        </div>
        <div v-if="savedInfo?.hasEndpoint" class="kv"><span class="k">Endpoint</span><span class="v break-all">{{ view.endpoint }}<span v-if="env.endpoint" class="envtag"> &middot; set by {{ env.endpoint }}</span></span></div>
        <template v-if="savedInfo?.hasAccount">
          <div class="kv"><span class="k">Account</span><span class="v break-all">{{ view.accountId || 'Not set' }}<span v-if="view.accountIdFromEnv" class="envtag"> &middot; from CLOUDFLARE_ACCOUNT_ID</span><span v-if="env.accountId" class="envtag"> &middot; set by {{ env.accountId }}</span></span></div>
          <div class="kv"><span class="k">Gateway</span><span class="v">{{ view.gateway || 'default' }}<span v-if="env.gateway" class="envtag"> &middot; set by {{ env.gateway }}</span></span></div>
        </template>
        <div class="row" style="gap: var(--sh-space-3); align-items: center;">
          <button class="btn btn-sm" type="button" data-testid="llm-change" @click="beginChange">Change provider or model</button>
          <span v-if="settingsMsg" class="sh-body" role="status" data-testid="llm-settings-saved" style="font-weight: 600; color: var(--sh-teal-lo);">{{ settingsMsg }}</span>
        </div>
      </div>

      <!-- Picker -->
      <form v-if="view && changing" class="col" style="gap: var(--sh-space-3);" data-testid="llm-form" @submit.prevent="saveSettings">
        <div class="field">
          <span id="llm-provider-l" class="field-label">Provider</span>
          <div class="opt-row prov-row" role="radiogroup" aria-labelledby="llm-provider-l" @keydown="onProviderKey">
            <button
              v-for="p in providers" :key="p.id" type="button" role="radio" class="opt-card"
              :class="{ on: form.provider === p.id }" :aria-checked="form.provider === p.id"
              :tabindex="form.provider === p.id ? 0 : -1" :disabled="!!env.provider || busy"
              :data-testid="`llm-provider-${p.id}`" @click="pickProvider(p.id)"
            >
              <span><span class="ot">{{ p.label }}</span><span class="od">{{ p.description }}</span></span>
            </button>
          </div>
          <div v-if="env.provider" class="help" data-testid="llm-env-provider-help">Set by <code>{{ env.provider }}</code>, so it can't be changed here.</div>
        </div>

        <div v-if="formInfo?.hasEndpoint" class="field">
          <label for="llm-endpoint">Endpoint</label>
          <input id="llm-endpoint" v-model="form.endpoint" class="field-input" type="url" spellcheck="false" autocomplete="off"
            :disabled="!!env.endpoint" data-testid="llm-endpoint" @change="maybeLoadModels" />
          <div v-if="env.endpoint" class="help">Set by <code>{{ env.endpoint }}</code>.</div>
          <div v-else-if="form.provider === 'ollama'" class="help">For Ollama Cloud use <code>https://ollama.com/v1</code> and save an API key below.</div>
        </div>

        <template v-if="formInfo?.hasAccount">
          <div class="field">
            <label for="llm-account">Account ID</label>
            <input id="llm-account" v-model="form.accountId" class="field-input" spellcheck="false" autocomplete="off"
              :placeholder="view.accountIdFromEnv ? 'Using CLOUDFLARE_ACCOUNT_ID' : '32-character account ID'"
              :disabled="!!env.accountId" :aria-invalid="!!accountError" aria-describedby="llm-account-h" data-testid="llm-account" />
            <div id="llm-account-h" class="help">
              <template v-if="env.accountId">Set by <code>{{ env.accountId }}</code>.</template>
              <template v-else-if="accountError"><span style="color: var(--sh-error);" role="alert">{{ accountError }}</span></template>
              <template v-else>Find it in the Cloudflare dashboard, or with <code>wrangler whoami</code>. Not a secret.</template>
            </div>
          </div>
          <div class="field">
            <label for="llm-gateway">Gateway <span class="sh-micro">(optional)</span></label>
            <input id="llm-gateway" v-model="form.gateway" class="field-input" spellcheck="false" autocomplete="off" placeholder="default"
              :disabled="!!env.gateway" :aria-invalid="!!gatewayError" data-testid="llm-gateway" />
            <div class="help">
              <template v-if="env.gateway">Set by <code>{{ env.gateway }}</code>.</template>
              <template v-else-if="gatewayError"><span style="color: var(--sh-error);" role="alert">{{ gatewayError }}</span></template>
              <template v-else>Leave blank to use your account's default gateway.</template>
            </div>
          </div>
        </template>

        <div class="field">
          <label for="llm-model">Model</label>
          <div class="row" style="gap: var(--sh-space-2); align-items: stretch;">
            <select v-if="ollamaSelect" id="llm-model" v-model="form.model" class="field-input" style="flex: 1;" :disabled="!!env.model" data-testid="llm-model">
              <option value="" disabled>Pick a model</option>
              <option v-for="m in ollamaOptions" :key="m" :value="m">{{ m }}</option>
            </select>
            <input v-else id="llm-model" v-model="form.model" class="field-input" style="flex: 1;" spellcheck="false" autocomplete="off"
              list="llm-model-list" :placeholder="modelPlaceholder" :disabled="!!env.model" data-testid="llm-model" />
            <button v-if="listsModels" class="btn btn-sm" type="button" :disabled="models.loading || busy" data-testid="llm-load-models" @click="loadModels">
              {{ models.loading ? 'Loading' : models.loaded ? 'Refresh' : 'Load models' }}
            </button>
          </div>
          <datalist id="llm-model-list"><option v-for="m in suggestions" :key="m" :value="m" /></datalist>
          <div v-if="env.model" class="help">Set by <code>{{ env.model }}</code>.</div>
          <template v-else>
            <div v-if="form.provider === 'cloudflare'" class="help">Use the <code>provider/model</code> form, for example <code>openai/gpt-5.5</code>, <code>anthropic/claude-sonnet-5-5</code> or a Workers AI model such as <code>@cf/moonshotai/kimi-k2.6</code>.</div>
            <div v-else-if="formInfo?.defaultModel" class="help">Leave blank to use <code>{{ formInfo.defaultModel }}</code>. Any model id the provider accepts works.</div>
          </template>
          <div v-if="form.provider === 'ollama' && models.loaded && models.unreachable" class="note warn" data-testid="llm-ollama-down">
            <Icon name="info" :size="16" />
            <span>Ollama isn't running at <code>{{ form.endpoint }}</code>. Start it (<code>ollama serve</code>), then Refresh.</span>
          </div>
          <div v-else-if="form.provider === 'ollama' && models.loaded && !models.error && !models.list.length" class="note warn" data-testid="llm-ollama-empty">
            <Icon name="info" :size="16" />
            <span>No models installed. Run <code>ollama pull qwen3:0.6b</code> (or any model that supports tools), then Refresh.</span>
          </div>
          <div v-else-if="models.loaded && models.error" class="note warn" data-testid="llm-models-error">
            <Icon name="info" :size="16" />
            <span>Couldn't load models: {{ models.error }}. You can still type a model id.</span>
          </div>
          <div v-if="form.provider === 'ollama' && !form.model" class="help" data-testid="llm-ollama-pick">Ollama has no default model: pick one.</div>
        </div>

        <div v-if="needsAccount" class="note warn" data-testid="llm-need-account">
          <Icon name="info" :size="16" />
          <span>Cloudflare needs an account ID before the agent can run. Add it here, or set <code>CLOUDFLARE_ACCOUNT_ID</code>.</span>
        </div>
        <div v-if="fileKeyHazard" class="note attn" data-testid="llm-file-key-warning">
          <Icon name="lock" :size="16" />
          <span>The plaintext key in <code>~/.vibe.yaml</code> would be sent to {{ formInfo?.label }}. Move it to the keychain for {{ savedInfo?.label }} first (below), or save a key for {{ formInfo?.label }}.</span>
        </div>

        <div class="row" style="gap: var(--sh-space-2); align-items: center;">
          <button class="btn btn-sm" type="submit" :disabled="busy || !!accountError || !!gatewayError || needsModel || keyPending" data-testid="llm-save-settings">Save</button>
          <span v-if="needsModel" class="help" data-testid="llm-save-why">Pick a model to save: Ollama has no default.</span>
          <button class="btn btn-sm" type="button" :disabled="busy" data-testid="llm-cancel-settings" @click="cancelChange">Cancel</button>
          <span v-if="settingsErr" class="sh-body" role="alert" data-testid="llm-settings-error" style="font-weight: 600; color: var(--sh-error);">{{ settingsErr }}</span>
        </div>
      </form>

      <div v-if="keyStatus" class="field" data-testid="llm-key">
        <label id="llm-key-l" for="llm-key-input">{{ keyLabel }}{{ keyOptionalNote }}<span v-if="changing && keyProviderArg" class="sh-micro"> &middot; for {{ keyInfo?.label }}</span></label>

        <!-- Saved in the keychain: write-only -->
        <div v-if="source === 'keychain' && !editing" class="cred" role="group" aria-labelledby="llm-key-l" data-testid="llm-key-saved">
          <span class="dots" aria-label="Saved, hidden">&bull;&bull;&bull;&bull;&bull;&bull;&bull;&bull;&bull;&bull;&bull;&bull;</span>
          <span class="where"><Icon name="shieldCheck" :size="14" /> In your keychain</span>
          <span class="acts">
            <button class="linkbtn" type="button" :disabled="busy" data-testid="llm-key-replace" @click="startReplace">Replace</button>
            <span class="sepdot" aria-hidden="true">&middot;</span>
            <button class="linkbtn danger" type="button" :disabled="busy" data-testid="llm-key-clear" @click="clearKey">Clear</button>
          </span>
        </div>

        <!-- Set by the environment: nothing to store -->
        <div v-else-if="source === 'env' && !editing" class="cred" role="group" aria-labelledby="llm-key-l" data-testid="llm-key-env">
          <span class="dots" aria-label="Set, hidden">&bull;&bull;&bull;&bull;&bull;&bull;&bull;&bull;&bull;&bull;&bull;&bull;</span>
          <span class="where"><Icon name="shieldCheck" :size="14" /> From an environment variable</span>
          <span class="acts" v-if="keyStatus.keychainAvailable">
            <button class="linkbtn" type="button" :disabled="busy" @click="startReplace">Save a key in the keychain</button>
          </span>
        </div>

        <!-- Not stored yet, or replacing -->
        <form v-if="showForm" class="row" style="gap: var(--sh-space-2); align-items: stretch;" @submit.prevent="save">
          <input
            id="llm-key-input" ref="keyInput" v-model="draft" type="password" class="field-input" style="flex: 1;"
            autocomplete="off" autocapitalize="off" spellcheck="false" placeholder="Paste a key to save it in your keychain"
            aria-describedby="llm-key-h" data-testid="llm-key-input"
          />
          <button class="btn btn-sm" type="submit" :disabled="busy || !draft.trim()" data-testid="llm-key-save">Save to keychain</button>
          <button v-if="editing" class="btn btn-sm" type="button" :disabled="busy" @click="editing = false; draft = ''">Cancel</button>
        </form>

        <div v-if="source === 'file'" class="help" data-testid="llm-key-file">The key in use is the plaintext one in <code>~/.vibe.yaml</code>. It is used for whichever provider is selected, so it is sent to {{ keyInfo?.label }}.</div>
        <div id="llm-key-h" class="help">The <code>VIBE_LLM_API_KEY</code> environment variable<template v-if="keyInfo?.keyEnvVars?.length">, or <template v-for="(v, i) in keyInfo.keyEnvVars" :key="v"><template v-if="i"> / </template><code>{{ v }}</code></template>,</template> if set, is used instead. The key is saved to your computer's keychain and Vibe can't show it again, only replace or clear it.</div>
        <div v-if="!keyStatus.keychainAvailable" class="note warn" data-testid="llm-key-nokeychain">
          <Icon name="lock" :size="16" />
          <span>Can't reach your keychain. Set <code>VIBE_LLM_API_KEY</code> (or your provider's usual variable) in the environment Vibe starts from. Vibe never writes a key to a file.</span>
        </div>
        <span v-if="message" class="sh-body" role="status" :style="{ fontWeight: 600, color: message.startsWith('Error') ? 'var(--sh-error)' : 'var(--sh-teal-lo)' }">{{ message }}</span>
      </div>

      <div v-if="keyStatus && keyStatus.canMigrate && source !== 'keychain'" class="note info" data-testid="llm-key-migrate">
        <Icon name="info" :size="16" />
        <span>Found a key in <code>~/.vibe.yaml</code>. <button class="linkbtn" type="button" style="padding: 0 4px;" :disabled="busy" data-testid="llm-key-migrate-btn" @click="migrate">Move it to the keychain</button> (as the key for {{ savedInfo?.label ?? 'the saved provider' }}) and blank the file. The rest of the file stays as it is.</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.prov-row { grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); }
.opt-card:focus-visible { box-shadow: var(--sh-focus-ring), var(--sh-shadow-teal); }
.field-input::placeholder { color: var(--sh-graphite); opacity: 1; }
.field-input:disabled { opacity: 0.6; cursor: not-allowed; }
.envtag { color: var(--sh-graphite); font-weight: 400; }
</style>
