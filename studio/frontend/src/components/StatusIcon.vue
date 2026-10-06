<script setup lang="ts">
// StatusIcon — a connection's state as shape AND colour AND an accessible name
// (SPEC 05): hollow circle, filled circle, alert, key, download arrow, padlock.
import { computed } from 'vue'
import Icon from './Icon.vue'
import { STATUS_META, statusKey } from '../lib/connections'

const props = defineProps<{ status: string; label?: string; size?: number }>()
const key = computed(() => statusKey(props.status))
const meta = computed(() => STATUS_META[key.value])
const text = computed(() => props.label ?? meta.value.label)
</script>

<template>
  <span class="st" :class="key" role="img" :aria-label="text" :title="text" :data-status="key">
    <Icon :name="meta.icon" :size="size ?? 14" />
  </span>
</template>
