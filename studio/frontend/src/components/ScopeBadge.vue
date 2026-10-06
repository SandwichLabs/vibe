<script setup lang="ts">
// ScopeBadge — who gets a connection: Team, This computer, All my workspaces.
import { computed } from 'vue'
import { SCOPE_LABEL, SCOPE_TIP } from '../lib/connections'

const props = defineProps<{ scope: ConnScope }>()
const text = computed(() => SCOPE_LABEL[props.scope])
const tip = computed(() => SCOPE_TIP[props.scope])
</script>

<template>
  <span class="scope" :class="scope" :title="tip" data-testid="scope-badge">{{ text }}</span>
</template>
