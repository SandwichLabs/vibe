<script setup lang="ts">
// TestResult — the outcome of a connection Test (SPEC 06): a success banner
// (role=status) or an error banner (role=alert) with a plain-language line, the
// redacted raw text behind "Show details", and an install button when an
// extension is what is missing. Focus moves here on a result.
import { ref, computed } from 'vue'
import Icon from './Icon.vue'

const props = defineProps<{ result: TestResultView | null; running: boolean; readOnly: boolean }>()
const emit = defineEmits<{ install: [ext: string] }>()
const el = ref<HTMLElement | null>(null)

const seconds = computed(() => (props.result ? (props.result.ms / 1000).toFixed(props.result.ms < 1000 ? 2 : 1) : ''))
const found = computed(() => {
  const r = props.result
  if (!r?.ok) return ''
  const base = r.summary ? `Found ${r.summary}.` : 'Connected.'
  return props.readOnly ? `${base} Read-only is on: Vibe will look but not change.` : base
})

defineExpose({ focus: () => el.value?.focus() })
</script>

<template>
  <div v-if="running" class="banner" role="status" aria-live="polite" data-testid="test-running">
    <div class="ico"><Icon name="zap" :size="22" /></div>
    <div class="body"><div class="ttl">Testing the connection…</div><div class="sub">This tries your settings without saving. It stops after 10 seconds.</div></div>
  </div>
  <div v-else-if="result && result.ok" ref="el" tabindex="-1" class="banner teal" role="status" data-testid="test-ok">
    <div class="ico"><Icon name="check" :size="22" /></div>
    <div class="body">
      <div class="ttl">Connected in {{ seconds }} seconds</div>
      <div class="sub">{{ found }}</div>
    </div>
  </div>
  <div v-else-if="result && result.error" ref="el" tabindex="-1" class="banner attn" role="alert" data-testid="test-error">
    <div class="ico"><Icon name="alert" :size="22" /></div>
    <div class="body">
      <div class="ttl">Couldn't connect</div>
      <div class="sub">{{ result.error.message }}</div>
      <button v-if="result.installExtension" type="button" class="btn btn-sm" style="margin-top: 8px;" data-testid="test-install" @click="emit('install', result.installExtension!)">Install the {{ result.installExtension }} extension…</button>
      <details v-if="result.error.details" style="margin-top: 8px;">
        <summary style="cursor: pointer; font-weight: 600; font-size: 13px;">Show details</summary>
        <div class="details" style="margin-top: 8px; overflow-wrap: anywhere;" data-testid="test-details">{{ result.error.details }}</div>
        <div class="help" style="margin-top: 6px;">Tested in {{ seconds }} s · secrets are removed from this text</div>
      </details>
    </div>
  </div>
</template>
