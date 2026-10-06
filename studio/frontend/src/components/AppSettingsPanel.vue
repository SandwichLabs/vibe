<script setup lang="ts">
// Settings > App (SPEC 09 B): what belongs to this computer, every workspace.
import AppearanceSettings from './AppearanceSettings.vue'
import AgentModelSettings from './AgentModelSettings.vue'
import ExtensionsPanel from './ExtensionsPanel.vue'
</script>

<template>
  <div id="settings-panel-app" role="tabpanel" aria-labelledby="settings-tab-app" class="col" style="max-width: 52rem; gap: var(--sh-space-6);">
    <AppearanceSettings />
    <AgentModelSettings />
    <ExtensionsPanel />
  </div>
</template>
