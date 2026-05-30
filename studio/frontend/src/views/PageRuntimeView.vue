<script setup lang="ts">
// PageRuntimeView (Phase 4 / P4.7) — the App-Mode runtime viewer for a bound
// page. The switchboard's open_page action must land an end-user on a *read*
// surface, NOT the builder editor (/pages/:id/edit is meta.builderOnly and is
// locked out in App Mode). This view renders the page HTML via RenderPageContent
// without exposing any schema/SQL/editing affordance, so it is safe to leave
// reachable in App Mode.
import { ref, watch, nextTick, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { execScripts } from '../lib/execScripts'

const route = useRoute()
const rendered = ref('')
const loading = ref(true)
const errorMessage = ref('')
const previewEl = ref<HTMLElement | null>(null)

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
    rendered.value = await window.go.main.App.RenderPageContent(id, {})
  } catch (e: any) {
    rendered.value = ''
    errorMessage.value = 'Failed to render page: ' + (e.message || e)
  }
  loading.value = false
}

watch(() => [route.params.id, route.query.id], render)
watch(rendered, () => { nextTick(() => execScripts(previewEl.value)) })

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
    ></div>
  </div>
</template>
