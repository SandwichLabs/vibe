<script setup lang="ts">
import { computed } from 'vue'
import { ICON_PATHS } from '../icons'

const props = withDefaults(defineProps<{
  name: string
  size?: number | string
}>(), { size: 16 })

const inner = computed(() => ICON_PATHS[props.name] ?? '')
</script>

<template>
  <svg
    :width="size"
    :height="size"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="2"
    stroke-linecap="round"
    stroke-linejoin="round"
    v-html="inner"
  ></svg>
</template>
