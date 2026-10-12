<script setup lang="ts">
// Settings, split in three (SPEC 09, #40): Workspace (what belongs to the folder and
// travels with git, plus MCP), App (what belongs to this computer: theme, the
// agent's model) and About (build, licence, notices). The selected tab is in the URL
// (?tab=app|about); arrow keys switch.
import { computed, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import WorkspaceSettingsPanel from '../components/WorkspaceSettingsPanel.vue'
import AppSettingsPanel from '../components/AppSettingsPanel.vue'
import AboutSettings from '../components/AboutSettings.vue'
import { workspaceInfo } from '../composables/useWorkspace'

const route = useRoute()
const router = useRouter()
const TABS = ['workspace', 'app', 'about'] as const
type Tab = (typeof TABS)[number]
const tab = computed<Tab>(() => TABS.find((t) => t === route.query.tab) ?? 'workspace')
const info = computed(() => workspaceInfo.value)

function select(t: Tab) {
  return router.replace({ path: route.path, query: { ...route.query, tab: t === 'workspace' ? undefined : t } })
}

async function onTabKey(e: KeyboardEvent) {
  if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(e.key)) return
  e.preventDefault()
  const i = TABS.indexOf(tab.value)
  const next = e.key === 'Home' ? 0 : e.key === 'End' ? TABS.length - 1 : (i + (e.key === 'ArrowRight' ? 1 : -1) + TABS.length) % TABS.length
  await select(TABS[next])
  await nextTick()
  document.getElementById(`settings-tab-${TABS[next]}`)?.focus()
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
      <button id="settings-tab-about" type="button" role="tab" aria-controls="settings-panel-about" :aria-selected="tab === 'about'" :tabindex="tab === 'about' ? 0 : -1" data-testid="settings-tab-about" @click="select('about')">
        About<span class="tsub">Version · licence · notices</span>
      </button>
    </div>

    <WorkspaceSettingsPanel v-if="tab === 'workspace'" />
    <AppSettingsPanel v-else-if="tab === 'app'" />
    <AboutSettings v-else />
  </div>
</template>
