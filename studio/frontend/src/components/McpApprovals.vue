<script setup lang="ts">
import { App } from '../lib/bindings'
// McpApprovals — approval cards for writes an external MCP client (Claude
// Desktop, Claude Code) asks Studio to make through Studio's own MCP server
// (turn "mcp"). Chat turns show their cards inline in the conversation; an MCP
// call has no conversation, so its card floats here until answered. Same card,
// same ResolveAgentApproval answer.
import { ref, onMounted, onUnmounted } from 'vue'
import { EventsOn } from '../wailsjs/runtime/runtime'
import ApprovalCard from './ApprovalCard.vue'
import type { PendingApproval } from '../composables/useAgentChat'
import { hasWailsRuntime } from '../lib/wails'

interface Item { approval: PendingApproval; tool: string }
const items = ref<Item[]>([])
const unsubs: Array<() => void> = []

onMounted(() => {
  if (!hasWailsRuntime()) return
  unsubs.push(
    EventsOn('agent:approval', (p: { turnId: string; request: any }) => {
      if (!p || p.turnId !== 'mcp') return
      const r = p.request
      items.value.push({
        tool: r.tool,
        approval: { id: r.id, summary: r.summary, sql: r.sql, where: r.where, destructive: !!r.destructive, connections: r.connections, rows: r.rows, state: 'pending' },
      })
    }),
    EventsOn('agent:approval_done', (p: { turnId: string; id: string; outcome: PendingApproval['state'] }) => {
      if (!p || p.turnId !== 'mcp') return
      const it = items.value.find((x) => x.approval.id === p.id)
      if (!it) return
      it.approval.state = p.outcome
      setTimeout(() => { items.value = items.value.filter((x) => x !== it) }, 2500)
    }),
    // A closed workspace cancels every pending card on the Go side.
    EventsOn('workspace:closed', () => { items.value = [] }),
  )
})
onUnmounted(() => unsubs.splice(0).forEach((u) => u()))

async function resolve(it: Item, approved: boolean) {
  it.approval.state = 'sending'
  try {
    await App.ResolveAgentApproval(it.approval.id, approved)
  } catch {
    if (it.approval.state === 'sending') it.approval.state = 'gone'
    setTimeout(() => { items.value = items.value.filter((x) => x !== it) }, 2500)
  }
}
</script>

<template>
  <div v-if="items.length" class="mcp-approvals" aria-live="polite" data-testid="mcp-approvals">
    <div v-for="it in items" :key="it.approval.id" class="card" style="padding: 10px 12px; box-shadow: var(--sh-shadow-lg);">
      <div class="sh-label" style="margin-bottom: 4px;">A connected tool wants to change something · {{ it.tool }}</div>
      <ApprovalCard :approval="it.approval" @resolve="(ok: boolean) => resolve(it, ok)" />
    </div>
  </div>
</template>
