<script setup lang="ts">
// StorageNeed — the storage line under a lake / table location (#130): which
// storage connection reads this URL, or what is missing, or why the URL cannot
// be read from storage at all. "Set up storage" asks the editor to open the
// storage form (useStorageHandoff); the button is a plain, focusable button.
import { computed } from 'vue'
import Icon from './Icon.vue'
import StatusIcon from './StatusIcon.vue'
import { STATUS_META, statusKey } from '../lib/connections'

const props = defineProps<{ need: StorageNeedView; testid?: string }>()
defineEmits<{ setup: [need: StorageNeedView] }>()
const statusText = computed(() => (props.need.match ? STATUS_META[statusKey(props.need.match.status)].label : ''))
</script>

<template>
  <div class="storage-need" :data-testid="testid ?? 'storage-need'">
    <template v-if="need.templated">
      <Icon name="info" :size="14" />
      <span>Written with environment variables, so it is checked when the connection opens.</span>
    </template>
    <template v-else-if="need.match">
      <StatusIcon :status="need.match.status" />
      <span>Read with <b data-testid="storage-match">{{ need.match.name }}</b> · {{ statusText }}</span>
    </template>
    <template v-else-if="need.suggest">
      <Icon name="alert" :size="14" />
      <span>Needs a storage connection for <code data-testid="storage-suggest">{{ need.suggest.scope || need.url }}</code></span>
      <button v-if="need.suggest.scope || need.suggest.bucket_scope" type="button" class="btn btn-sm" data-testid="storage-setup" @click="$emit('setup', need)">Set up storage</button>
    </template>
    <template v-else-if="need.problem">
      <Icon name="alert" :size="14" />
      <span data-testid="storage-problem">{{ need.problem }}</span>
    </template>
  </div>
</template>

<style scoped>
.storage-need { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; margin-top: 6px; font-size: 13px; color: var(--vibe-fg); }
</style>
