<script setup lang="ts">
// AssertionResults — the outcome of "Run assertions": one row per check with
// pass, fail (the rows that broke it) or error (the check could not run, which
// says nothing about the data). The error text is shown as text.
import { computed } from 'vue'

const props = defineProps<{ rows: AssertionRow[] }>()
const summary = computed(() => {
  const n = (s: string) => props.rows.filter((r) => r.status === s).length
  return { pass: n('pass'), fail: n('fail'), error: n('error') }
})
const badge = (s: AssertionRow['status']) => (s === 'pass' ? 'badge--success' : s === 'fail' ? 'badge--error' : 'badge--warn')
</script>

<template>
  <div class="col assertion-results" data-testid="assertion-results">
    <p class="help" style="margin: 0;" data-testid="assertion-summary">
      {{ summary.pass }} passed<template v-if="summary.fail">, {{ summary.fail }} failed</template><template v-if="summary.error">, {{ summary.error }} could not run</template>.
    </p>
    <table class="table">
      <thead><tr><th>Subject</th><th>Check</th><th>Result</th><th class="cell-num">Violations</th></tr></thead>
      <tbody>
        <tr v-for="(r, i) in rows" :key="i" :data-testid="`assertion-row-${i}`">
          <td><code>{{ r.subject }}</code></td>
          <td>{{ r.assertion }}<template v-if="r.column"> on <code>{{ r.column }}</code></template></td>
          <td>
            <span class="badge" :class="badge(r.status)"><span class="dot"></span>{{ r.status }}</span>
            <div v-if="r.error" class="help" style="margin: 4px 0 0; color: var(--sh-error);">{{ r.error }}</div>
          </td>
          <td class="cell-num">{{ r.status === 'error' ? '' : r.violations.toLocaleString() }}</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.assertion-results { gap: var(--sh-space-2); }
</style>
