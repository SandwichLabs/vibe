<script setup lang="ts">
// RunStatus — progress and errors of a definition run: a line while it works
// (with the seconds it has taken, since a check can run for minutes) and the
// error or conflict message when it stopped. Text only.
defineProps<{ busy: boolean; elapsed: number; error: string; conflict: string; what: string }>()
</script>

<template>
  <div class="col run-status">
    <p v-if="busy" class="help" style="margin: 0;" role="status" aria-live="polite" data-testid="run-progress">
      <span class="run-status__spin" aria-hidden="true"></span> {{ what }}… {{ elapsed }} s
    </p>
    <div v-if="error" class="note attn" role="alert" data-testid="run-error"><span>{{ error }}</span></div>
    <div v-if="conflict" class="note warn" role="alert" data-testid="run-conflict"><span>{{ conflict }}</span></div>
  </div>
</template>

<style scoped>
.run-status { gap: var(--sh-space-2); }
.run-status__spin {
  display: inline-block; width: 10px; height: 10px; margin-right: 4px;
  border: 2px solid var(--sh-teal); border-right-color: transparent; border-radius: 50%;
  animation: run-spin 0.9s linear infinite;
}
@keyframes run-spin { to { transform: rotate(360deg); } }
@media (prefers-reduced-motion: reduce) { .run-status__spin { animation: none; } }
</style>
