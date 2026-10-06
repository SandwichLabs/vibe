<script setup lang="ts">
// Settings, split in two (SPEC 09): Workspace (what belongs to the folder and
// travels with git, plus MCP) and App (what belongs to this computer: theme, the
// agent's model). The selected tab is in the URL (?tab=app); arrow keys switch.
import { computed, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import WorkspaceSettingsPanel from '../components/WorkspaceSettingsPanel.vue'
import AppSettingsPanel from '../components/AppSettingsPanel.vue'
import { workspaceInfo } from '../composables/useWorkspace'

const route = useRoute()
const router = useRouter()
const tab = computed<'workspace' | 'app'>(() => (route.query.tab === 'app' ? 'app' : 'workspace'))
const info = computed(() => workspaceInfo.value)

function select(t: 'workspace' | 'app') {
  router.replace({ path: route.path, query: { ...route.query, tab: t === 'app' ? 'app' : undefined } })
}

async function onTabKey(e: KeyboardEvent) {
  if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(e.key)) return
  e.preventDefault()
  const next = tab.value === 'workspace' ? 'app' : 'workspace'
  select(e.key === 'Home' ? 'workspace' : e.key === 'End' ? 'app' : next)
  await nextTick()
  document.getElementById(`settings-tab-${tab.value}`)?.focus()
}
</script>

<template>
  <div style="height: 100%; overflow-y: auto; padding: 32px;">
    <div class="headrow">
      <div>
        <div class="crumb">Studio <span class="sep">/</span> Settings</div>
        <h1>Settings</h1>
      </div>
    </div>

    <div class="tabs" role="tablist" aria-label="Settings" @keydown="onTabKey">
      <button id="settings-tab-workspace" type="button" role="tab" aria-controls="settings-panel-workspace" :aria-selected="tab === 'workspace'" :tabindex="tab === 'workspace' ? 0 : -1" data-testid="settings-tab-workspace" @click="select('workspace')">
        Workspace<span class="tsub">{{ info?.name ?? '' }} · saved in this folder</span>
      </button>
      <button id="settings-tab-app" type="button" role="tab" aria-controls="settings-panel-app" :aria-selected="tab === 'app'" :tabindex="tab === 'app' ? 0 : -1" data-testid="settings-tab-app" @click="select('app')">
        App<span class="tsub">This computer · every workspace</span>
      </button>
    </div>

    <WorkspaceSettingsPanel v-if="tab === 'workspace'" />
    <AppSettingsPanel v-else />
  </div>
</template>
