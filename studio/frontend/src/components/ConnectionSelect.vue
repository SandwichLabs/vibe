<script setup lang="ts">
// ConnectionSelect — the "Run on" choice (SPEC 08), shared by the query editor
// (a toolbar button) and notebook SQL cells (a compact chip). It is per editor
// and per cell, never session-global: plain table names run on the connection
// picked here; qualified names (prod_pg.public.customers) work whatever it is,
// and may span connections. A combobox/listbox: Enter or Down opens, arrows
// move, Enter picks, Escape closes.
import { ref, computed, nextTick, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import Icon from './Icon.vue'
import StatusIcon from './StatusIcon.vue'
import ScopeBadge from './ScopeBadge.vue'
import ConnTypeTile from './ConnTypeTile.vue'
import { connectionList, defaultConnectionName, connect, startConnectionTracking, refreshConnections } from '../composables/useConnections'
import { useMenuKeys } from '../composables/useMenuKeys'

const props = defineProps<{
  // The chosen connection; '' means the workspace default.
  modelValue: string
  variant?: 'toolbar' | 'cell'
  disabled?: boolean
}>()
const emit = defineEmits<{ 'update:modelValue': [name: string] }>()

const router = useRouter()
const cell = computed(() => props.variant === 'cell')
const open = ref(false)
const btn = ref<HTMLButtonElement | null>(null)
const menuEl = ref<HTMLElement | null>(null)
const pos = ref({ left: 0, top: 0 })
const active = ref(0)
const uid = Math.random().toString(36).slice(2, 8)

interface Opt { value: string; label: string; conn?: ConnectionInfo; isDefault?: boolean; disabled?: boolean }
const options = computed<Opt[]>(() => {
  const list: Opt[] = []
  if (cell.value) list.push({ value: '', label: 'Workspace default', isDefault: true })
  for (const c of connectionList.value) {
    if (c.catalogless) continue // storage scopes have no tables to run on
    list.push({ value: c.name, label: c.name, conn: c, disabled: c.status === 'locked-by' })
  }
  return list
})

const current = computed(() => connectionList.value.find((c) => c.name === (props.modelValue || defaultConnectionName.value)))
const explicit = computed(() => props.modelValue !== '')
const chipText = computed(() => (explicit.value ? props.modelValue : `Default · ${defaultConnectionName.value}`))
const label = computed(() => (cell.value ? `Connection for this cell: ${chipText.value}` : `Run on: ${props.modelValue || defaultConnectionName.value}`))

function place() {
  const r = btn.value?.getBoundingClientRect()
  if (!r) return
  const w = 320
  pos.value = { left: Math.max(8, Math.min(r.left, window.innerWidth - w - 8)), top: Math.min(r.bottom + 4, window.innerHeight - 340) }
}

async function show() {
  if (props.disabled) return
  place()
  open.value = true
  const i = options.value.findIndex((o) => o.value === props.modelValue)
  active.value = i < 0 ? 0 : i
  await nextTick()
  menuEl.value?.focus()
}
function hide(restore = true) {
  open.value = false
  if (restore) void nextTick(() => btn.value?.focus())
}

async function pick(o: Opt) {
  if (o.disabled) return
  hide()
  emit('update:modelValue', o.value)
  const c = o.conn
  if (c && c.status !== 'ok') void connect(c.name)
}

function onBtnKey(e: KeyboardEvent) {
  if (e.key === 'ArrowDown' || e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.stopPropagation(); void show() }
}
const onMenuKey = useMenuKeys({
  enabled: () => options.value.map((o) => !o.disabled),
  current: () => active.value,
  moveTo: (i) => { active.value = i },
  close: (restore) => hide(restore),
  tab: 'leave',
  choose: (i) => { const o = options.value[i]; if (o) void pick(o) },
})
function onDoc(e: MouseEvent) {
  if (open.value && !(e.target as HTMLElement).closest(`[data-conn-select="${uid}"]`)) hide(false)
}
function manage() { hide(false); void router.push('/connections/new') }

onMounted(() => { startConnectionTracking(); if (!connectionList.value.length) void refreshConnections(); document.addEventListener('mousedown', onDoc) })
onUnmounted(() => document.removeEventListener('mousedown', onDoc))
</script>

<template>
  <span class="conn-select-wrap" :data-conn-select="uid" style="display: inline-flex;">
    <button
      ref="btn"
      type="button"
      class="conn-select"
      :class="{ cell }"
      aria-haspopup="listbox"
      :aria-expanded="open"
      :aria-label="label"
      :disabled="disabled"
      :title="cell ? 'Saved with the cell, so a teammate runs it on the same connection.' : 'Run on'"
      data-testid="conn-select"
      @click="open ? hide() : show()"
      @keydown="onBtnKey"
    >
      <span v-if="!cell" class="lab">Run on</span>
      <ConnTypeTile v-if="current && (!cell || explicit)" :type="current.type" />
      <span :data-testid="'conn-select-value'">{{ cell ? chipText : (modelValue || defaultConnectionName) }}</span>
      <Icon name="chevUpDown" :size="14" class="cv" />
    </button>

    <div
      v-if="open"
      ref="menuEl"
      class="menu floating"
      role="listbox"
      tabindex="-1"
      :aria-label="cell ? 'Connection for this cell' : 'Run on'"
      :aria-activedescendant="`cs-${uid}-${active}`"
      :style="{ left: pos.left + 'px', top: pos.top + 'px', width: '320px' }"
      data-testid="conn-select-menu"
      @keydown="onMenuKey"
    >
      <div class="mhead">{{ cell ? 'Run this cell on' : 'Run on' }}</div>
      <template v-for="(o, i) in options" :key="o.value || '__default'">
        <div
          :id="`cs-${uid}-${i}`"
          role="option"
          class="mi"
          :class="{ active: o.value === modelValue, disabled: o.disabled, kf: active === i }"
          :aria-selected="o.value === modelValue"
          :aria-disabled="o.disabled ? 'true' : undefined"
          :data-testid="`conn-opt-${o.value || 'default'}`"
          @click="pick(o)"
          @mouseenter="active = i"
        >
          <template v-if="o.isDefault">
            <Icon name="home" :size="16" />
            <span class="grow"><span class="t">Workspace default</span><span class="sub">{{ defaultConnectionName }}</span></span>
            <span v-if="modelValue === ''" class="end"><Icon name="check" :size="14" /></span>
          </template>
          <template v-else>
            <ConnTypeTile :type="o.conn!.type" />
            <span class="grow"><span class="t">{{ o.label }}</span><span v-if="o.disabled" class="sub">In use by another program</span></span>
            <span class="end">
              <Icon v-if="o.conn!.readOnly && o.conn!.type !== 'motherduck'" name="lock" :size="12" aria-label="Read-only" />
              <ScopeBadge :scope="o.conn!.scope" />
              <StatusIcon :status="o.conn!.status" />
              <Icon v-if="o.value === modelValue" name="check" :size="14" />
            </span>
          </template>
        </div>
      </template>
      <div class="msep" role="separator"></div>
      <div class="mnote">Plain table names run on the connection you pick. To use several at once, write <code>connection.schema.table</code>, like <code>prod_pg.public.customers</code>.<template v-if="cell"> Saved with the cell, so a teammate runs it on the same connection.</template></div>
      <button type="button" class="mi" style="width: 100%;" @click="manage"><Icon name="plug" :size="16" /><span class="grow"><span class="t">Manage connections…</span></span></button>
    </div>
  </span>
</template>
