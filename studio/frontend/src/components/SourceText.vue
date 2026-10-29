<script setup lang="ts">
// SourceText — a definition's file exactly as stored, folded away until
// opened. Shown as text, never interpreted. Useful most when the file could not
// be read: the text is there to read and fix.
defineProps<{ text: string; path: string; open?: boolean }>()
</script>

<template>
  <details class="source-text" :open="open" data-testid="definition-source">
    <summary class="help" style="cursor: pointer;">File: <code>{{ path }}</code></summary>
    <pre class="sh-mono source-text__pre">{{ text }}</pre>
  </details>
</template>

<style scoped>
.source-text__pre { white-space: pre-wrap; margin: var(--sh-space-2) 0 0; padding: var(--sh-space-3); background: var(--sh-paper-2); border: var(--sh-border-thin); border-radius: var(--sh-radius-sm); max-height: 280px; overflow: auto; }
</style>
