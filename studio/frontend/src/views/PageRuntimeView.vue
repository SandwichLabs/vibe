<script setup lang="ts">
// PageRuntimeView (Phase 4 / P4.7) — the App-Mode runtime viewer for a bound
// page. The switchboard's open_page action must land an end-user on a *read*
// surface, NOT the builder editor (/pages/:id/edit is meta.builderOnly and is
// locked out in App Mode). This view renders the page HTML via RenderPageContent
// without exposing any schema/SQL/editing affordance, so it is safe to leave
// reachable in App Mode.
import { ref, watch, nextTick, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { execScripts } from '../lib/execScripts'
import { wireInputFilters } from '../lib/wireInputFilters'
import { useMacroIntents, type MacroResult } from '../composables/useMacroIntents'

const route = useRoute()
const router = useRouter()
const rendered = ref('')
const loading = ref(true)
const errorMessage = ref('')
const previewEl = ref<HTMLElement | null>(null)

// Input-filter state (P3.6 parity with PageEditorView): rendered {dropdown}/
// {slider}/{toggle}/… controls must drive a re-render with their values as
// page params in App Mode too, not just in the Studio editor preview.
const filterParams = ref<Record<string, string>>({})

function onFilterChange(name: string, value: string) {
  if (value === '') {
    delete filterParams.value[name]
  } else {
    filterParams.value[name] = value
  }
  render()
}

// Shared macro intent dispatcher (P5.6) — the SAME applier the AppShell
// switchboard uses, so open_*/set_filter/msg_box behave identically here.
const { msgBox, applyMacroIntents } = useMacroIntents(() => router)

// runningMacroId guards against double-fires: while a command button's RunMacro
// is in-flight the button is disabled.
const runningMacroId = ref<string | null>(null)

// onMacroButtonClick is delegated from the rendered page container. P5.5 emits
// command buttons as <button class="vibe-button" data-macro-id="…"> with no
// inline JS; here we detect the data-macro-id hook, call App.RunMacro(target),
// and apply the returned ordered intents through the shared applier.
async function onMacroButtonClick(ev: Event) {
  const target = ev.target as HTMLElement | null
  const btn = target?.closest('button[data-macro-id]') as HTMLButtonElement | null
  if (!btn) return
  const macroId = btn.getAttribute('data-macro-id') || ''
  if (!macroId) return
  // In-flight guard: ignore re-entrant clicks while a macro is running.
  if (runningMacroId.value) return

  runningMacroId.value = macroId
  btn.disabled = true
  try {
    const result = (await window.go.main.App.RunMacro(macroId)) as MacroResult
    applyMacroIntents(result?.intents || [])
  } catch (e: any) {
    errorMessage.value = 'Failed to run macro: ' + (e.message || e)
  } finally {
    runningMacroId.value = null
    btn.disabled = false
  }
}

function pageId(): string {
  const id = route.params.id ?? route.query.id
  return Array.isArray(id) ? id[0] ?? '' : (id ?? '') as string
}

async function render() {
  const id = pageId()
  if (!id) {
    rendered.value = ''
    loading.value = false
    return
  }
  loading.value = true
  errorMessage.value = ''
  try {
    rendered.value = await window.go.main.App.RenderPageContent(id, { ...filterParams.value })
  } catch (e: any) {
    rendered.value = ''
    errorMessage.value = 'Failed to render page: ' + (e.message || e)
  }
  loading.value = false
}

watch(() => [route.params.id, route.query.id], () => {
  filterParams.value = {}
  render()
})
watch(rendered, () => {
  nextTick(() => {
    execScripts(previewEl.value)
    wireInputFilters(previewEl.value, onFilterChange)
  })
})

onMounted(render)
</script>

<template>
  <div class="page-runtime" style="height: 100%; overflow: auto; padding: var(--sh-space-6);">
    <div v-if="loading" class="sh-mono" style="color: var(--sh-graphite);">Rendering…</div>
    <p v-else-if="errorMessage" class="sh-error" role="alert">{{ errorMessage }}</p>
    <div
      v-else
      ref="previewEl"
      v-html="rendered"
      class="prose vibe-content"
      style="max-width: none;"
      data-testid="page-runtime-content"
      @click="onMacroButtonClick"
    ></div>

    <!-- MsgBox dialog raised by a run_macro msg_box intent (shared applier). -->
    <div
      v-if="msgBox !== null"
      data-testid="macro-msgbox"
      class="macro-msgbox sh-card"
      role="dialog"
    >
      <p class="macro-msgbox-text">{{ msgBox }}</p>
      <button class="sh-btn" @click="msgBox = null">OK</button>
    </div>
  </div>
</template>
