<script setup lang="ts">
import { App } from '../lib/bindings'
// ExtensionsPanel — App settings > Extensions (SPEC 09): one row per DuckDB
// extension a connection type needs: what it is for, which connections here use
// it, whether it is installed, and an Install button (the consent for the
// download is the click). Stored in Vibe's own folder.
import { ref, onMounted, onUnmounted } from 'vue'
import Icon from './Icon.vue'
import { EventsOn } from '../wailsjs/runtime/runtime'
import { errorText } from '../lib/errors'
import { refreshConnections } from '../composables/useConnections'
import { hasWailsRuntime } from '../lib/wails'

const rows = ref<ExtensionInfo[]>([])
const busy = ref('')
const error = ref('')
const loaded = ref(false)
const unsubs: Array<() => void> = []

async function load() {
  try {
    rows.value = (await App.ListExtensions()) ?? []
  } catch (e: unknown) {
    error.value = errorText(e)
  }
  loaded.value = true
}

async function install(name: string) {
  busy.value = name
  error.value = ''
  try {
    await App.InstallExtension({ name })
    await refreshConnections()
  } catch (e: unknown) {
    error.value = errorText(e)
  }
  busy.value = ''
  await load()
}

onMounted(() => {
  void load()
  if (hasWailsRuntime()) {
    unsubs.push(EventsOn('extension:progress', () => { void load() }))
  }
})
onUnmounted(() => unsubs.splice(0).forEach((u) => u()))
</script>

<template>
  <section data-testid="extensions-panel" aria-labelledby="ext-h">
    <div class="sec-title">
      <h2 id="ext-h" style="font-size: 22px;">Extensions</h2>
      <span class="label">Downloaded once, with your OK</span>
    </div>
    <div class="card card-pad col" style="gap: var(--sh-space-3);">
      <p class="help" style="margin: 0;">Some connection types need a small add-on for Vibe's database engine. Vibe never downloads one without asking: install it here, or when a connection asks for it.</p>
      <table class="brutal-table">
        <caption class="sr">DuckDB extensions</caption>
        <thead><tr><th scope="col">Extension</th><th scope="col">What it is for</th><th scope="col">Used by</th><th scope="col">Status</th><th scope="col"><span class="sr">Action</span></th></tr></thead>
        <tbody>
          <tr v-for="e in rows" :key="e.name" data-testid="ext-row">
            <td><code>{{ e.name }}</code></td>
            <td>{{ e.purpose }}</td>
            <td>{{ e.neededBy.length ? e.neededBy.join(', ') : 'No connection here' }}</td>
            <td>
              <span class="badge" style="white-space: nowrap;" :class="e.installed ? 'badge--success' : ''" :data-testid="`ext-status-${e.name}`">
                <Icon :name="e.installed ? 'circleCheck' : 'download'" :size="12" /> {{ e.installed ? 'Installed' : 'Not installed' }}
              </span>
            </td>
            <td>
              <button v-if="!e.installed" type="button" class="btn btn-sm" :disabled="busy !== ''" :data-testid="`ext-install-${e.name}`" @click="install(e.name)">{{ busy === e.name ? 'Installing…' : 'Install' }}</button>
            </td>
          </tr>
          <tr v-if="loaded && !rows.length"><td colspan="5">No extensions are needed.</td></tr>
        </tbody>
      </table>
      <p class="help" style="margin: 0;">Stored in Vibe's own folder, separate from any DuckDB you installed yourself. DuckDB cannot uninstall an extension, so there is no Remove button here yet; deleting Vibe's extensions folder removes them all.</p>
      <div v-if="error" class="note attn" role="alert"><Icon name="alert" :size="16" /><span>{{ error }}</span></div>
    </div>
  </section>
</template>
