<script setup lang="ts">
// SignInCode — the code of a running AWS sign-in, in a <code> the person checks
// against the page that opened, with Copy and the address as text (never a link:
// the page was opened by the app, after its address was checked). Used by the
// banner and the connection editor's card.
import { ref } from 'vue'

const props = defineProps<{ prompt: SignInPrompt; testid?: string }>()
const copied = ref(false)
async function copy() {
  try {
    await navigator.clipboard.writeText(props.prompt.code)
    copied.value = true
    setTimeout(() => { copied.value = false }, 1500)
  } catch { /* clipboard unavailable: the code is selectable */ }
}
</script>

<template>
  <span class="sic">
    <code class="sic-code" :data-testid="testid ?? 'signin-code'">{{ prompt.code }}</code>
    <button type="button" class="btn btn-sm" :data-testid="(testid ?? 'signin-code') + '-copy'" @click="copy">{{ copied ? 'Copied' : 'Copy code' }}</button>
    <span class="sic-url help">Page: <code :data-testid="(testid ?? 'signin-code') + '-url'">{{ prompt.url }}</code></span>
  </span>
</template>

<style scoped>
.sic { display: inline-flex; flex-wrap: wrap; align-items: center; gap: 8px 10px; margin-top: 4px; }
.sic-code {
  font-family: var(--sh-font-mono); font-size: 20px; font-weight: 700; letter-spacing: 0.12em;
  padding: 2px 10px; background: var(--sh-surface); color: var(--sh-ink);
  border: 2px solid var(--vibe-border); border-radius: var(--sh-radius-sm); user-select: all;
}
.sic-url { flex-basis: 100%; overflow-wrap: anywhere; }
</style>
