<script setup lang="ts">
// SchemaBuilderView (parity W3) — the conversational Schema Builder: a chat
// pane where the agent PROPOSES a schema (via the propose_schema tool, which
// never mutates the database) beside a live ER diagram of the real schema.
// Proposed tables render as dashed "appearing" cards over the committed ones;
// "Looks good — build it" creates them through the existing CreateTable /
// AddForeignKey bindings, "Discard changes" drops the draft. The old
// form-based designer remains at /schema/fields for per-field properties.
import { ref, computed, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { useRouter } from 'vue-router'
import Icon from '../components/Icon.vue'
import { useAgentChat } from '../composables/useAgentChat'
import {
  parseDraft, toSQLType, toFriendlyType, autoLayout, cardHeight, edgePoint,
  CARD_W, type SchemaDraft, type Pos, type Rect,
} from '../lib/schemaDraft'

const router = useRouter()

// ---- Live (committed) schema ----
interface LiveColumn { name: string; type: string; pk: boolean }
interface LiveTable { name: string; columns: LiveColumn[] }
interface LiveRel {
  parent_table: string; parent_column: string
  child_table: string; child_column: string
  enforce_integrity?: boolean
}

const liveTables = ref<LiveTable[]>([])
const liveRels = ref<LiveRel[]>([])
const loading = ref(true)
const errorMessage = ref('')
const statusMessage = ref('')

async function loadSchema() {
  loading.value = true
  try {
    const names: string[] = (await window.go.main.App.ListTables()) ?? []
    const tables: LiveTable[] = []
    for (const name of names) {
      try {
        const info = await window.go.main.App.DescribeTable(name)
        tables.push({
          name,
          columns: (info?.columns ?? []).map((c: any) => ({
            name: c.name,
            type: c.type,
            // DescribeTable doesn't surface PKs; the id-column convention is
            // how the diagram vocabulary marks the key.
            pk: Boolean(c.pk) || c.name === 'id',
          })),
        })
      } catch { /* table may have vanished mid-load */ }
    }
    liveTables.value = tables
    liveRels.value = ((await window.go.main.App.ListRelationships()) ?? []) as LiveRel[]
  } catch (e: any) {
    errorMessage.value = 'Failed to load schema: ' + (e.message || e)
  }
  loading.value = false
}

// ---- Agent chat (left pane) ----
const chat = useAgentChat()
const { messages, sending, renderMarkdown } = chat
const draftText = ref('')
const streamEl = ref<HTMLElement | null>(null)

async function scrollToBottom() {
  await nextTick()
  if (streamEl.value) streamEl.value.scrollTop = streamEl.value.scrollHeight
}

async function submit() {
  const text = draftText.value
  if (!text.trim() || sending.value) return
  draftText.value = ''
  await send(text)
}
const { send } = chat

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    submit()
  }
}

// ---- Draft (proposed) schema ----
const draft = ref<SchemaDraft | null>(null)
// The last propose_schema payload we applied — each proposal carries the FULL
// draft, so a new payload replaces the old draft wholesale.
let lastProposalInput = ''

watch(messages, () => {
  let latest = ''
  for (const m of messages.value) {
    for (const s of m.steps) {
      if (s.tool === 'propose_schema' && s.input) latest = s.input
    }
  }
  if (latest && latest !== lastProposalInput) {
    const parsed = parseDraft(latest)
    if (parsed) {
      lastProposalInput = latest
      draft.value = parsed
    }
  }
}, { deep: true })

const liveNames = computed(() => new Set(liveTables.value.map((t) => t.name.toLowerCase())))
// Proposed tables that don't already exist — the ones a commit will create.
const pendingTables = computed(() =>
  (draft.value?.tables ?? []).filter((t) => !liveNames.value.has(t.name.toLowerCase())))

function discard() {
  draft.value = null
  statusMessage.value = 'Draft discarded'
}

const building = ref(false)

// buildIt commits the draft through the real DDL bindings: create every new
// table first (parents and children alike — Link columns are plain BIGINTs),
// then declare the FKs, which rebuild each child table with the constraint.
async function buildIt() {
  if (!draft.value || building.value) return
  building.value = true
  errorMessage.value = ''
  try {
    for (const t of pendingTables.value) {
      await window.go.main.App.CreateTable(t.name, t.columns.map((c) => ({
        name: c.name,
        type: toSQLType(c.type),
        pk: Boolean(c.primary_key),
        not_null: Boolean(c.not_null),
      })))
    }
    const already = new Set(liveRels.value.map((r) =>
      `${r.child_table}.${r.child_column}->${r.parent_table}.${r.parent_column}`.toLowerCase()))
    for (const r of draft.value.relationships) {
      const key = `${r.child_table}.${r.child_column}->${r.parent_table}.${r.parent_column}`.toLowerCase()
      if (already.has(key)) continue
      await window.go.main.App.AddForeignKey({
        child_table: r.child_table,
        child_column: r.child_column,
        parent_table: r.parent_table,
        parent_column: r.parent_column,
        on_delete: 'RESTRICT',
        on_update: 'NO ACTION',
      })
    }
    const built = pendingTables.value.length
    draft.value = null
    statusMessage.value = `Built ${built} table(s)`
    await loadSchema()
  } catch (e: any) {
    errorMessage.value = 'Build failed: ' + (e.message || e)
  }
  building.value = false
}

// ---- Diagram (right pane) ----
interface Card {
  name: string
  proposed: boolean
  rows: { name: string; ty: string; pk: boolean }[]
}

const fkColumns = computed(() => {
  const set = new Set<string>()
  for (const r of liveRels.value) set.add(`${r.child_table}.${r.child_column}`.toLowerCase())
  for (const r of draft.value?.relationships ?? []) set.add(`${r.child_table}.${r.child_column}`.toLowerCase())
  return set
})

const cards = computed<Card[]>(() => {
  const out: Card[] = liveTables.value.map((t) => ({
    name: t.name,
    proposed: false,
    rows: t.columns.map((c) => ({
      name: c.name,
      ty: toFriendlyType(c.type, { pk: c.pk, fk: fkColumns.value.has(`${t.name}.${c.name}`.toLowerCase()) }),
      pk: c.pk,
    })),
  }))
  for (const t of pendingTables.value) {
    out.push({
      name: t.name,
      proposed: true,
      rows: t.columns.map((c) => ({
        name: c.name,
        ty: c.primary_key ? 'ID' : (fkColumns.value.has(`${t.name}.${c.name}`.toLowerCase()) ? 'Link' : toFriendlyType(toSQLType(c.type), {})),
        pk: Boolean(c.primary_key),
      })),
    })
  }
  return out
})

const positions = ref<Record<string, Pos>>({})

watch(cards, (cs) => {
  const heights: Record<string, number> = {}
  for (const c of cs) heights[c.name] = cardHeight(c.rows.length)
  // Drop positions of cards that no longer exist, keep dragged ones.
  const kept: Record<string, Pos> = {}
  for (const c of cs) if (positions.value[c.name]) kept[c.name] = positions.value[c.name]
  positions.value = autoLayout(cs.map((c) => c.name), (n) => heights[n] ?? cardHeight(1), kept)
}, { immediate: true })

function rectOf(name: string): Rect | null {
  const p = positions.value[name]
  const c = cards.value.find((x) => x.name === name)
  if (!p || !c) return null
  return { x: p.x, y: p.y, w: CARD_W, h: cardHeight(c.rows.length) }
}

interface ArrowLine { x1: number; y1: number; x2: number; y2: number; dashed: boolean; marigold: boolean; key: string }

const arrows = computed<ArrowLine[]>(() => {
  const out: ArrowLine[] = []
  const all: { rel: LiveRel | any; dashed: boolean; marigold: boolean }[] = []
  for (const r of liveRels.value) {
    // Metadata-only (unenforced) links draw marigold dashed, like optional
    // links in the mockup; enforced FKs are solid teal.
    const soft = r.enforce_integrity === false
    all.push({ rel: r, dashed: soft, marigold: soft })
  }
  for (const r of draft.value?.relationships ?? []) {
    all.push({ rel: r, dashed: Boolean(r.optional), marigold: Boolean(r.optional) })
  }
  for (const { rel, dashed, marigold } of all) {
    const child = rectOf(rel.child_table)
    const parent = rectOf(rel.parent_table)
    if (!child || !parent) continue
    const childCenter = { x: child.x + child.w / 2, y: child.y + child.h / 2 }
    const parentCenter = { x: parent.x + parent.w / 2, y: parent.y + parent.h / 2 }
    const from = edgePoint(child, parentCenter)
    const to = edgePoint(parent, childCenter)
    out.push({
      x1: from.x, y1: from.y, x2: to.x, y2: to.y, dashed, marigold,
      key: `${rel.child_table}.${rel.child_column}->${rel.parent_table}.${rel.parent_column}`,
    })
  }
  return out
})

const canvasHeight = computed(() => {
  let max = 480
  for (const c of cards.value) {
    const p = positions.value[c.name]
    if (p) max = Math.max(max, p.y + cardHeight(c.rows.length) + 48)
  }
  return max
})

// ---- Card dragging ----
let dragName: string | null = null
let dragOff: Pos = { x: 0, y: 0 }
const canvasEl = ref<HTMLElement | null>(null)

function startDrag(name: string, e: PointerEvent) {
  const p = positions.value[name]
  if (!p) return
  dragName = name
  const host = canvasEl.value?.getBoundingClientRect()
  dragOff = { x: e.clientX - (host?.left ?? 0) - p.x, y: e.clientY - (host?.top ?? 0) - p.y }
  window.addEventListener('pointermove', onDragMove)
  window.addEventListener('pointerup', endDrag)
}

function onDragMove(e: PointerEvent) {
  if (!dragName) return
  const host = canvasEl.value?.getBoundingClientRect()
  positions.value = {
    ...positions.value,
    [dragName]: {
      x: Math.max(0, e.clientX - (host?.left ?? 0) - dragOff.x),
      y: Math.max(0, e.clientY - (host?.top ?? 0) - dragOff.y),
    },
  }
}

function endDrag() {
  dragName = null
  window.removeEventListener('pointermove', onDragMove)
  window.removeEventListener('pointerup', endDrag)
}

onUnmounted(endDrag)

// Auto-dismiss toasts, matching the other views.
watch(errorMessage, (m) => { if (m) setTimeout(() => { errorMessage.value = '' }, 4000) })
watch(statusMessage, (m) => { if (m) setTimeout(() => { statusMessage.value = '' }, 3000) })

onMounted(() => {
  loadSchema()
  chat.setScrollHandler(scrollToBottom)
  // Steer the agent toward the propose -> review -> commit flow on this screen.
  chat.pin('the Schema Builder screen (/schema). When the user describes their business or asks for schema design or changes, call propose_schema with the COMPLETE draft (all tables + relationships, friendly field types); it renders on their live ER diagram and they commit it with the "Looks good — build it" button. Do not create tables directly unless explicitly asked to build immediately.')
})
</script>

<template>
  <div class="col" style="height: 100%; gap: 0;">
    <!-- Subhead: crumb, draft badge, discard / build actions -->
    <div class="page-subhead" style="padding: 14px 24px;">
      <div>
        <div class="crumb">Studio <span class="sep">/</span> Schema</div>
        <h1 style="font-size: 24px; margin: 4px 0 0;">Describe your business.</h1>
      </div>
      <span class="spacer"></span>
      <span
        v-if="pendingTables.length > 0"
        class="badge badge--warn"
        data-testid="schema-draft-badge"
      >Draft · {{ pendingTables.length }} table{{ pendingTables.length === 1 ? '' : 's' }}</span>
      <button class="btn btn-sm" data-testid="schema-field-props" @click="router.push('/schema/fields')">
        <Icon name="settings" :size="14" /> Field properties
      </button>
      <button
        v-if="draft"
        class="btn btn-sm"
        data-testid="schema-discard"
        @click="discard"
      >Discard changes</button>
      <button
        class="btn btn-primary btn-sm"
        data-testid="schema-build"
        :disabled="pendingTables.length === 0 || building"
        @click="buildIt"
      >
        {{ building ? 'Building…' : 'Looks good — build it' }} <Icon name="arrowRight" :size="14" />
      </button>
    </div>

    <div class="builder-grid">
      <!-- LEFT — conversational designer -->
      <div class="chat-pane">
        <div ref="streamEl" class="chat-stream" data-testid="schema-chat-stream">
          <div v-if="messages.length === 0" class="chat-empty">
            <Icon name="sparkle" :size="20" />
            <p>
              Describe your business and the agent will sketch a schema on the diagram —
              tables, fields, and the links between them. Nothing is created until you press
              <strong>Looks good — build it</strong>.
            </p>
            <p class="hint">“I run a small HVAC company. We do service calls — customers schedule them,
              a technician shows up, logs parts used, and we bill the customer.”</p>
          </div>

          <div v-for="(m, i) in messages" :key="i" class="amsg">
            <div class="av" :class="m.role === 'user' ? 'user' : 'bot'">
              {{ m.role === 'user' ? 'You' : 'AI' }}
            </div>
            <div class="body">
              <div class="who">
                {{ m.role === 'user' ? 'You' : m.role === 'error' ? 'Agent · error' : (m.streaming ? 'Agent · streaming' : 'Agent') }}
              </div>
              <div v-if="m.role === 'user'" class="user-bubble">{{ m.text }}</div>
              <p v-else-if="m.role === 'error'" class="chat-error">{{ m.text }}</p>
              <div v-else-if="m.text" class="chat-md" v-html="renderMarkdown(m.text)"></div>
              <p v-else-if="m.streaming" class="chat-dim">Thinking…</p>

              <div v-for="(s, si) in m.steps" :key="si" class="tool-chip" data-testid="schema-tool-chip">
                <span class="lab"><Icon name="wrench" :size="12" /> {{ s.tool }}</span>
                <span v-if="s.status === 'running'" class="spin" data-testid="chip-running">◐</span>
                <span v-else class="check" data-testid="chip-done">✓</span>
              </div>
            </div>
          </div>
        </div>

        <div class="composer">
          <textarea
            v-model="draftText"
            class="composer-box"
            data-testid="schema-chat-input"
            placeholder="Add details, ask for changes, or paste an old spreadsheet schema…"
            @keydown="onKeydown"
          ></textarea>
          <div class="row" style="justify-content: space-between; margin-top: 8px;">
            <span class="composer-hint">⏎ Send · ⇧⏎ Newline</span>
            <button
              class="btn btn-primary btn-sm"
              data-testid="schema-chat-send"
              :disabled="sending || !draftText.trim()"
              @click="submit"
            >
              <Icon name="send" :size="13" /> {{ sending ? 'Sending…' : 'Send' }}
            </button>
          </div>
        </div>
      </div>

      <!-- RIGHT — live ER diagram -->
      <div class="diagram-pane">
        <div ref="canvasEl" class="diagram-canvas" :style="{ height: canvasHeight + 'px', minHeight: '100%' }">
          <div class="canvas-head">
            <span class="canvas-label">Live schema<template v-if="draft?.note"> · {{ draft.note }}</template></span>
            <span class="badge badge--teal"><Icon name="sparkle" :size="11" /> Editable</span>
          </div>

          <div v-if="loading" class="canvas-label" style="position: absolute; top: 56px; left: 24px;">Loading schema…</div>
          <div
            v-else-if="cards.length === 0"
            class="canvas-empty"
          >
            <Icon name="columns" :size="40" />
            <p>No tables yet — describe your business in the chat and a schema will appear here.</p>
          </div>

          <!-- FK arrows (under the cards) -->
          <svg class="arrows" :width="'100%'" :height="canvasHeight">
            <defs>
              <marker id="arr-teal" markerWidth="9" markerHeight="8" refX="8" refY="4" orient="auto">
                <path d="M0,0 L8,4 L0,8 z" class="head-teal" />
              </marker>
              <marker id="arr-marigold" markerWidth="9" markerHeight="8" refX="8" refY="4" orient="auto">
                <path d="M0,0 L8,4 L0,8 z" class="head-marigold" />
              </marker>
            </defs>
            <line
              v-for="a in arrows"
              :key="a.key"
              data-testid="schema-arrow"
              :x1="a.x1" :y1="a.y1" :x2="a.x2" :y2="a.y2"
              :class="a.marigold ? 'line-marigold' : 'line-teal'"
              :stroke-dasharray="a.dashed ? '6 4' : undefined"
              :marker-end="a.marigold ? 'url(#arr-marigold)' : 'url(#arr-teal)'"
            />
          </svg>

          <!-- Table cards -->
          <div
            v-for="c in cards"
            :key="c.name"
            class="tbl"
            :class="{ appearing: c.proposed }"
            data-testid="schema-card"
            :data-table="c.name"
            :style="{ position: 'absolute', left: (positions[c.name]?.x ?? 0) + 'px', top: (positions[c.name]?.y ?? 0) + 'px', zIndex: 1, cursor: 'grab', touchAction: 'none' }"
            @pointerdown="startDrag(c.name, $event)"
          >
            <div class="ttl">{{ c.name }}</div>
            <div v-for="r in c.rows" :key="r.name" class="row" :class="{ pk: r.pk }">
              <span class="nm"><Icon v-if="r.pk" name="key" :size="11" />{{ r.name }}</span>
              <span class="ty">{{ r.ty }}</span>
            </div>
          </div>
        </div>
      </div>
    </div>

    <div v-if="errorMessage" class="error-toast" role="alert">{{ errorMessage }}</div>
    <div v-if="statusMessage" class="status-toast" data-testid="schema-status">{{ statusMessage }}</div>
  </div>
</template>

<style scoped>
.builder-grid {
  flex: 1;
  display: grid;
  grid-template-columns: 6fr 5fr;
  min-height: 0;
}
.chat-pane {
  border-right: 2px solid var(--sh-ink);
  display: flex;
  flex-direction: column;
  min-height: 0;
  min-width: 0;
}
.chat-stream {
  flex: 1;
  overflow-y: auto;
  min-height: 0;
  padding: 24px;
  display: flex;
  flex-direction: column;
  gap: 18px;
}
.chat-empty {
  color: var(--sh-graphite);
  font-size: 14px;
  line-height: 1.55;
  display: flex;
  flex-direction: column;
  gap: 10px;
  max-width: 520px;
}
.chat-empty .hint {
  font-style: italic;
  font-size: 13px;
  border-left: 3px solid var(--sh-teal);
  padding-left: 10px;
}
.chat-error { color: var(--sh-coral, #e5604a); }
.chat-dim { color: var(--sh-graphite); font-style: italic; }
.chat-md :deep(p) { margin: 0 0 6px; }
.chat-md :deep(pre) {
  background: var(--sh-ink);
  color: var(--sh-paper);
  padding: 8px 10px;
  border-radius: var(--sh-radius-sm);
  overflow-x: auto;
  font-family: var(--sh-font-mono);
  font-size: 12px;
}
.chat-md :deep(code) { font-family: var(--sh-font-mono); font-size: 12px; }
.chat-md :deep(ul), .chat-md :deep(ol) { margin: 4px 0; padding-left: 20px; }
.composer {
  border-top: 2px solid var(--sh-ink);
  padding: 14px;
  background: var(--sh-paper-2);
}
.composer-box {
  width: 100%;
  background: var(--sh-surface);
  border: 2px solid var(--sh-ink);
  border-radius: var(--sh-radius-sm);
  padding: 12px 14px;
  min-height: 72px;
  font-size: 14px;
  font-family: inherit;
  color: var(--sh-ink);
  resize: vertical;
  outline: none;
}
.composer-hint {
  font-family: var(--sh-font-mono);
  font-size: 10px;
  color: var(--sh-graphite);
  text-transform: uppercase;
  letter-spacing: 0.06em;
}
.diagram-pane {
  position: relative;
  min-height: 0;
  overflow: auto;
}
.canvas-head {
  position: absolute;
  top: 14px;
  left: 20px;
  right: 20px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  z-index: 2;
  pointer-events: none;
}
.canvas-label {
  font-family: var(--sh-font-mono);
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--sh-graphite);
}
.canvas-empty {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  color: var(--sh-graphite);
  font-size: 14px;
  text-align: center;
  padding: 0 40px;
}
.arrows {
  position: absolute;
  inset: 0;
  pointer-events: none;
  z-index: 0;
}
.line-teal { stroke: var(--sh-teal); stroke-width: 2; }
.line-marigold { stroke: var(--sh-marigold); stroke-width: 2; }
.head-teal { fill: var(--sh-teal); }
.head-marigold { fill: var(--sh-marigold); }
/* Sub-pixel niceties for dragging */
.tbl { user-select: none; }
.tbl:active { cursor: grabbing; }
.status-toast {
  position: fixed;
  bottom: 24px;
  right: 24px;
  background: var(--sh-teal);
  color: var(--sh-paper);
  border: 2px solid var(--sh-ink);
  border-radius: var(--sh-radius-sm);
  padding: 10px 16px;
  font-size: 13px;
  font-weight: 600;
  z-index: 1000;
}
</style>
