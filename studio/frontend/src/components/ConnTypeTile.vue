<script setup lang="ts">
// ConnTypeTile — the two-letter tile that stands for a connection type.
import { typeCode } from '../lib/connections'

defineProps<{ type: string; large?: boolean }>()
</script>

<template>
  <span class="ctype" :class="{ lg: large }" aria-hidden="true">{{ typeCode(type) }}</span>
</template>
