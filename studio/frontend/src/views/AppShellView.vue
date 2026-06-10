<script setup lang="ts">
// AppShellView — the Switchboard (parity-plan §4.2/§W4; mockup #5 sw-main
// builder / #6 app-desktop runtime).
//
// One view, two responsibilities driven by the live app mode:
//
//   (a) Studio Mode — the W4 spatial builder: a 3-pane surface with a source
//       palette (forms / reports / pages grouped), a .sw-canvas of .sw-node
//       tiles hanging off a distinct home node with connector arrows, and a
//       selected-item inspector (display name / icon / sort order / access
//       rules). "Publish shell" persists the whole structure via SaveNav;
//       "Preview as worker" shows the canvas as the locked shell would.
//
//   (b) App Mode — the runtime switchboard: render each nav item as a large
//       44px-hit-target marigold button that routes to the bound form / report
//       / page, honoring startup_object as the initial landing screen and the
//       W4 visible_to access rules.
import { ref, computed, watch, onMounted, onUnmounted, getCurrentInstance } from 'vue'
import Icon from '../components/Icon.vue'
import { useAppMode } from '../composables/useAppMode'
import { useMacroIntents, type MacroIntent } from '../composables/useMacroIntents'
import { destinationFor, iconFor, visibleInAppMode, type NavItem } from '../lib/nav'

interface NavTarget {
  kind: string
  id: string
  label: string
}

// Access constraint: at most 8 items per switchboard page.
const MAX_PER_PAGE = 8

// Inspector icon choices (SchoolHouse icon set).
const ICON_CHOICES = [
  'form', 'users', 'calendar', 'clipboard', 'barChart', 'pieChart',
  'fileText', 'apps', 'home', 'play', 'database', 'star',
]

const { isAppMode, isStudio } = useAppMode()

// Reach the router. At runtime vue-router exposes it on the instance proxy as
// `$router`; the unit spec injects the same `$router` mock so navigation is
// observable without standing up a full router.
const instance = getCurrentInstance()
function router(): { push: (to: unknown) => unknown } | undefined {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  return (instance?.proxy as any)?.$router
}

const navItems = ref<NavItem[]>([])
const targets = ref<NavTarget[]>([])
const errorMessage = ref('')
const statusMessage = ref('')

// Builder state: the currently-edited switchboard page (root = ''), selection,
// preview toggle, and the publish-dirty snapshot.
const currentPage = ref('')
const selectedId = ref<string | null>(null)
const previewWorker = ref(false)
const savedJSON = ref('[]')
const publishing = ref(false)

// Items belonging to the switchboard page being edited / rendered.
const pageItems = computed(() =>
  navItems.value
    .filter((n) => n.parent_id === currentPage.value)
    .slice()
    .sort((a, b) => a.sort_order - b.sort_order),
)

// App-Mode (and worker-preview) items: access rules applied.
const runtimeItems = computed(() => pageItems.value.filter(visibleInAppMode))

const atLimit = computed(() => pageItems.value.length >= MAX_PER_PAGE)
const dirty = computed(() => JSON.stringify(navItems.value) !== savedJSON.value)
const selectedItem = computed(() =>
  navItems.value.find((n) => n.id === selectedId.value) ?? null)

// Palette: bindable objects grouped the way the mockup groups them.
const paletteGroups = computed(() => {
  const groups: { key: string; title: string; icon: string; targets: NavTarget[] }[] = [
    { key: 'open_form', title: 'Forms', icon: 'form', targets: [] },
    { key: 'open_report', title: 'Reports', icon: 'barChart', targets: [] },
    { key: 'open_page', title: 'Pages & dashboards', icon: 'pieChart', targets: [] },
  ]
  for (const t of targets.value) {
    const g = groups.find((x) => x.key === t.kind)
    if (g) g.targets.push(t)
  }
  return groups.filter((g) => g.targets.length > 0)
})

async function load() {
  errorMessage.value = ''
  try {
    navItems.value = (await window.go.main.App.ListNav()) || []
    savedJSON.value = JSON.stringify(navItems.value)
  } catch (err) {
    errorMessage.value = `Failed to load navigation: ${err}`
    navItems.value = []
  }
  try {
    targets.value = (await window.go.main.App.ListNavTargets()) || []
  } catch (err) {
    errorMessage.value = `Failed to load nav targets: ${err}`
    targets.value = []
  }
}

// addFromPalette drops a palette object onto the canvas as a new nav item and
// selects it so the inspector opens on it.
function addFromPalette(tgt: NavTarget) {
  if (atLimit.value) return
  const item: NavItem = {
    id: `nav_${tgt.id}_${Date.now()}`,
    parent_id: currentPage.value,
    label: tgt.label,
    action_kind: tgt.kind,
    target_id: tgt.id,
    sort_order: pageItems.value.length,
    icon: '',
    visible_to: '',
  }
  navItems.value.push(item)
  selectedId.value = item.id
}

// removeSelected deletes the inspected item and re-packs sort_order so the
// remaining items stay 0..n-1 contiguous.
function removeSelected() {
  if (!selectedItem.value) return
  navItems.value = navItems.value.filter((n) => n.id !== selectedId.value)
  selectedId.value = null
  renumber()
}

// setSortOrder moves the selected item to the requested position among its
// siblings and renumbers, so the inspector's Sort order field is authoritative.
function setSortOrder(item: NavItem, pos: number) {
  const siblings = pageItems.value.filter((n) => n.id !== item.id)
  const idx = Math.max(0, Math.min(siblings.length, Math.floor(pos)))
  siblings.splice(idx, 0, item)
  siblings.forEach((n, i) => { n.sort_order = i })
}

// renumber rewrites sort_order to a contiguous 0..n-1 sequence per switchboard
// page, honouring the current visual order.
function renumber() {
  const pages = new Map<string, NavItem[]>()
  for (const n of navItems.value) {
    const list = pages.get(n.parent_id) ?? []
    list.push(n)
    pages.set(n.parent_id, list)
  }
  for (const list of pages.values()) {
    list
      .slice()
      .sort((a, b) => a.sort_order - b.sort_order)
      .forEach((n, i) => { n.sort_order = i })
  }
}

// publish persists the whole switchboard structure (SaveNav reconciles —
// removals included).
async function publish() {
  errorMessage.value = ''
  publishing.value = true
  try {
    await window.go.main.App.SaveNav(navItems.value.map((n) => ({ ...n })))
    savedJSON.value = JSON.stringify(navItems.value)
    statusMessage.value = 'Shell published'
  } catch (err) {
    errorMessage.value = `Failed to save navigation: ${err}`
  } finally {
    publishing.value = false
  }
}

// --- Canvas geometry ---
// Nodes hang off the home node in rows of three across the measured canvas
// width; arrows connect home's bottom edge to each node's top edge.
const canvasEl = ref<HTMLElement | null>(null)
const canvasW = ref(900)

function measure() {
  if (canvasEl.value) canvasW.value = canvasEl.value.clientWidth || 900
}

const NODE_W = 210
const NODE_H = 64
const HOME_Y = 40

const homePos = computed(() => ({ x: canvasW.value / 2 - NODE_W / 2, y: HOME_Y }))

function nodePos(idx: number): { x: number; y: number } {
  const cols = 3
  const col = idx % cols
  const row = Math.floor(idx / cols)
  const pad = 28
  const span = Math.max(canvasW.value - 2 * pad - NODE_W, 0)
  const x = pad + (cols === 1 ? span / 2 : (span / (cols - 1)) * col)
  return { x, y: HOME_Y + 150 + row * 130 }
}

interface CanvasArrow { x1: number; y1: number; x2: number; y2: number; restricted: boolean; key: string; lx: number; ly: number }

const arrows = computed<CanvasArrow[]>(() =>
  pageItems.value.map((item, idx) => {
    const p = nodePos(idx)
    const from = { x: homePos.value.x + NODE_W / 2, y: homePos.value.y + NODE_H }
    const to = { x: p.x + NODE_W / 2, y: p.y }
    return {
      x1: from.x, y1: from.y, x2: to.x, y2: to.y,
      restricted: !visibleInAppMode(item),
      key: item.id,
      lx: (from.x + to.x) / 2,
      ly: (from.y + to.y) / 2 - 6,
    }
  }))

const canvasH = computed(() => {
  const rows = Math.max(1, Math.ceil(pageItems.value.length / 3))
  return HOME_Y + 150 + rows * 130 + 40
})

// kindLabel renders the node's sub-line ("Form", "Report", …).
function kindLabel(kind: string): string {
  switch (kind) {
    case 'open_form': return 'Form'
    case 'open_report': return 'Report'
    case 'open_page': return 'Page'
    case 'open_switchboard': return 'Switchboard'
    case 'run_macro': return 'Macro'
    default: return kind
  }
}

// destinationFor (route mapping) is shared with the App-Mode tab strip — see
// lib/nav.ts — so the locked-shell tabs and these buttons never diverge.

// Shared macro intent dispatcher (P5.6) — the SAME applier the page/form runtime
// uses, so there is exactly one dispatcher. macroMsgBox holds the message for a
// MsgBox dialog raised by a run_macro intent.
const { msgBox: macroMsgBox, applyMacroIntents } = useMacroIntents(router)

async function activate(item: NavItem) {
  // run_macro items dispatch through the backend macro runner and apply the
  // returned UI intents (P5.4).
  if (item.action_kind === 'run_macro') {
    try {
      const result = await window.go.main.App.RunMacro(item.target_id)
      applyMacroIntents((result?.intents as MacroIntent[]) || [])
    } catch (err) {
      errorMessage.value = `Failed to run macro: ${err}`
    }
    return
  }

  const dest = destinationFor(item)
  if (!dest) return
  router()?.push(dest)
}

onMounted(async () => {
  await load()
  measure()
  window.addEventListener('resize', measure)

  // AutoExec (P5.7 §5.3): run the configured startup macro on file open and
  // apply its UI intents (e.g. open_switchboard). The backend also emits an
  // "app:autoexec" event on OpenFile/CreateFile for shells already mounted; this
  // mount-time call covers the shell-mounts-after-open ordering. Both paths feed
  // the SAME applyMacroIntents dispatcher.
  try {
    const auto = await window.go.main.App.RunAutoExec()
    applyMacroIntents((auto?.intents as MacroIntent[]) || [])
  } catch (err) {
    errorMessage.value = `Failed to run AutoExec: ${err}`
  }

  // App Mode: honor startup_object as the initial landing screen.
  if (isAppMode.value) {
    try {
      const cfg = await window.go.main.App.GetAppConfig()
      const startup = cfg?.startup_object
      if (startup) {
        const item = navItems.value.find((n) => n.target_id === startup)
        if (item) activate(item)
      }
    } catch {
      // No startup landing if config is unavailable; the switchboard still renders.
    }
  }
})

onUnmounted(() => window.removeEventListener('resize', measure))

watch(statusMessage, (m) => { if (m) setTimeout(() => { statusMessage.value = '' }, 3000) })
</script>

<template>
  <section class="app-shell-view" :class="{ 'is-builder': isStudio }">
    <!-- App Mode: runtime switchboard (access rules applied) -->
    <div v-if="isAppMode" class="switchboard-runtime" style="padding: var(--sh-space-4, 1rem);">
      <div class="switchboard-grid">
        <button
          v-for="item in runtimeItems"
          :key="item.id"
          data-testid="switchboard-button"
          class="switchboard-button sh-card"
          style="min-height: 44px; background: var(--sh-marigold);"
          @click="activate(item)"
        >
          <Icon :name="iconFor(item)" :size="20" />
          <span class="switchboard-button-label">{{ item.label }}</span>
        </button>
      </div>
      <p v-if="!runtimeItems.length" class="sh-muted">No navigation items defined.</p>

      <!-- MsgBox dialog raised by a run_macro msg_box intent (P5.4). -->
      <div
        v-if="macroMsgBox !== null"
        data-testid="macro-msgbox"
        class="macro-msgbox sh-card"
        role="dialog"
      >
        <p class="macro-msgbox-text">{{ macroMsgBox }}</p>
        <button class="sh-btn" @click="macroMsgBox = null">OK</button>
      </div>
    </div>

    <!-- Studio Mode: W4 spatial switchboard builder -->
    <div v-else-if="isStudio" class="switchboard-builder">
      <div class="page-subhead" style="padding: 14px 24px;">
        <div>
          <div class="crumb">Studio <span class="sep">/</span> App Shell</div>
          <h1 style="font-size: 24px; margin: 4px 0 0;">Switchboard</h1>
        </div>
        <span class="spacer"></span>
        <span v-if="dirty" class="badge badge--warn" data-testid="shell-dirty">Unsaved</span>
        <button
          class="btn btn-sm"
          data-testid="preview-worker"
          @click="previewWorker = !previewWorker"
        >
          <Icon name="eye" :size="14" /> {{ previewWorker ? 'Exit preview' : 'Preview as worker' }}
        </button>
        <button
          class="btn btn-primary btn-sm"
          data-testid="publish-shell"
          :disabled="publishing"
          @click="publish"
        >
          {{ publishing ? 'Publishing…' : 'Publish shell' }}
        </button>
      </div>

      <!-- Worker preview: the canvas as the locked shell renders it (access
           rules applied), without flipping the real mode. -->
      <div v-if="previewWorker" class="worker-preview" data-testid="worker-preview">
        <div class="preview-banner">
          <Icon name="eye" :size="14" />
          Previewing as a worker — owner-only items are hidden. Buttons are inert.
        </div>
        <div class="switchboard-grid" style="margin-top: 16px;">
          <div
            v-for="item in runtimeItems"
            :key="item.id"
            data-testid="preview-button"
            class="switchboard-button sh-card"
            style="min-height: 44px; background: var(--sh-marigold); cursor: default;"
          >
            <Icon :name="iconFor(item)" :size="20" />
            <span class="switchboard-button-label">{{ item.label }}</span>
          </div>
        </div>
        <p v-if="!runtimeItems.length" class="sh-muted" style="margin-top: 16px;">
          Nothing is visible to workers yet.
        </p>
      </div>

      <div v-else class="builder-panes">
        <!-- LEFT — source palette -->
        <div class="col-pane palette">
          <div class="pane-label">Add to shell</div>
          <div v-for="g in paletteGroups" :key="g.key" class="palette-group">
            <div class="group-label">{{ g.title }}</div>
            <div
              v-for="t in g.targets"
              :key="t.id"
              class="tpl-row"
              data-testid="palette-target"
              :class="{ disabled: atLimit }"
              role="button"
              tabindex="0"
              @click="addFromPalette(t)"
              @keyup.enter="addFromPalette(t)"
            >
              <div class="ico"><Icon :name="g.icon" :size="16" /></div>
              <div class="name">{{ t.label }}</div>
              <Icon name="plus" :size="14" />
            </div>
          </div>
          <p v-if="paletteGroups.length === 0" class="sh-muted" style="font-size: 13px;">
            No forms, reports, or pages to add yet — build some first.
          </p>
          <p v-if="atLimit" data-testid="nav-limit" class="nav-limit sh-muted">
            This switchboard page is full (max {{ MAX_PER_PAGE }} items).
            Create a sub-page to add more.
          </p>
        </div>

        <!-- CENTER — switchboard canvas -->
        <div ref="canvasEl" class="sw-canvas" data-testid="sw-canvas" :style="{ minHeight: canvasH + 'px' }">
          <div class="canvas-label">Switchboard canvas · worker's view</div>

          <svg class="sw-arrows" :height="canvasH" width="100%">
            <defs>
              <marker id="sw-arr" markerWidth="9" markerHeight="8" refX="8" refY="4" orient="auto">
                <path d="M0,0 L8,4 L0,8 z" class="sw-arr-head" />
              </marker>
              <marker id="sw-arr-teal" markerWidth="9" markerHeight="8" refX="8" refY="4" orient="auto">
                <path d="M0,0 L8,4 L0,8 z" class="sw-arr-head-teal" />
              </marker>
            </defs>
            <g v-for="a in arrows" :key="a.key">
              <line
                data-testid="sw-arrow"
                :x1="a.x1" :y1="a.y1" :x2="a.x2" :y2="a.y2"
                :class="a.restricted ? 'sw-line-restricted' : 'sw-line'"
                :stroke-dasharray="a.restricted ? '6 4' : undefined"
                :marker-end="a.restricted ? 'url(#sw-arr-teal)' : 'url(#sw-arr)'"
              />
              <text v-if="a.restricted" :x="a.lx" :y="a.ly" class="sw-arrow-label">owner only</text>
            </g>
          </svg>

          <!-- Home node (distinct) -->
          <div
            class="sw-node home"
            data-testid="sw-home"
            :style="{ position: 'absolute', left: homePos.x + 'px', top: homePos.y + 'px', width: NODE_W + 'px' }"
          >
            <div class="ttl"><Icon name="home" :size="14" /> Home</div>
            <div class="sub">App start screen</div>
          </div>

          <!-- Item nodes -->
          <div
            v-for="(item, idx) in pageItems"
            :key="item.id"
            class="sw-node"
            data-testid="sw-node"
            :class="{ selected: selectedId === item.id }"
            :style="{ position: 'absolute', left: nodePos(idx).x + 'px', top: nodePos(idx).y + 'px', width: NODE_W + 'px', cursor: 'pointer' }"
            role="button"
            tabindex="0"
            @click="selectedId = item.id"
            @keyup.enter="selectedId = item.id"
          >
            <div class="ttl"><Icon :name="iconFor(item)" :size="14" /> {{ item.label }}</div>
            <div class="sub">
              {{ kindLabel(item.action_kind) }}<template v-if="!visibleInAppMode(item)"> · owner only</template>
            </div>
          </div>

          <p v-if="pageItems.length === 0" class="canvas-empty sh-muted">
            Pick a form, report, or page from the left to add it to the shell.
          </p>
        </div>

        <!-- RIGHT — inspector -->
        <div class="col-pane right inspector" data-testid="sw-inspector">
          <template v-if="selectedItem">
            <div class="pane-label teal">Selected · {{ selectedItem.label }}</div>

            <label class="field">
              <span class="field-label">Display name</span>
              <input
                v-model="selectedItem.label"
                data-testid="insp-label"
                class="field-input"
                type="text"
              />
            </label>

            <label class="field">
              <span class="field-label">Icon</span>
              <select v-model="selectedItem.icon" data-testid="insp-icon" class="field-input">
                <option value="">Auto ({{ kindLabel(selectedItem.action_kind) }})</option>
                <option v-for="ic in ICON_CHOICES" :key="ic" :value="ic">{{ ic }}</option>
              </select>
            </label>

            <label class="field">
              <span class="field-label">Sort order</span>
              <input
                :value="selectedItem.sort_order"
                data-testid="insp-sort"
                class="field-input"
                type="number"
                min="0"
                :max="pageItems.length - 1"
                @change="setSortOrder(selectedItem, Number(($event.target as HTMLInputElement).value))"
              />
            </label>

            <div class="advanced">
              <div class="pane-label">Advanced · access rules</div>
              <label class="field">
                <span class="field-label">Visible to</span>
                <select v-model="selectedItem.visible_to" data-testid="insp-visible-to" class="field-input">
                  <option value="">Everyone</option>
                  <option value="owner">Owner only (hidden in App Mode)</option>
                </select>
              </label>
              <p class="sh-muted" style="font-size: 12px; line-height: 1.45;">
                Owner-only items never render in the locked App Mode shell.
                Per-person rules arrive with multi-user (v1.1).
              </p>
            </div>

            <span class="spacer-v"></span>
            <button class="btn btn-sm btn-danger" data-testid="insp-remove" @click="removeSelected">
              <Icon name="trash" :size="14" /> Remove from shell
            </button>
          </template>
          <template v-else>
            <div class="pane-label">Inspector</div>
            <p class="sh-muted" style="font-size: 13px; line-height: 1.5;">
              Select a tile on the canvas to edit its name, icon, order, and
              access rules.
            </p>
          </template>
        </div>
      </div>

      <p v-if="errorMessage" class="sh-error" style="padding: 0 24px 12px;">{{ errorMessage }}</p>
      <div v-if="statusMessage" class="status-toast" data-testid="shell-status">{{ statusMessage }}</div>
    </div>
  </section>
</template>

<style scoped>
.app-shell-view {
  height: 100%;
  display: flex;
  flex-direction: column;
  min-height: 0;
}
.switchboard-builder {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-height: 0;
}
.builder-panes {
  flex: 1;
  display: grid;
  grid-template-columns: 230px 1fr 270px;
  min-height: 0;
}
.col-pane {
  background: var(--sh-paper-2);
  border-right: 2px solid var(--sh-ink);
  padding: 16px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.col-pane.right {
  border-right: 0;
  border-left: 2px solid var(--sh-ink);
}
.pane-label {
  font-family: var(--sh-font-mono);
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--sh-graphite);
}
.pane-label.teal { color: var(--sh-teal); }
.palette-group .group-label {
  font-family: var(--sh-font-mono);
  font-size: 10px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--sh-graphite);
  margin: 10px 0 6px;
}
.tpl-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px;
  margin-bottom: 6px;
  background: var(--sh-surface);
  border: 1.5px solid var(--sh-ink);
  border-radius: var(--sh-radius-sm);
  box-shadow: 1px 1px 0 0 var(--sh-ink);
  cursor: pointer;
  font-size: 13px;
}
.tpl-row:hover { background: var(--sh-teal-8); }
.tpl-row.disabled { opacity: 0.45; cursor: not-allowed; }
.tpl-row .ico {
  width: 28px;
  height: 28px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: 1.5px solid var(--sh-ink);
  border-radius: var(--sh-radius-sm);
  background: var(--sh-paper);
  flex: none;
}
.tpl-row .name { flex: 1; font-weight: 600; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.sw-canvas { position: relative; overflow: auto; }
.canvas-label {
  position: absolute;
  top: 14px;
  left: 16px;
  font-family: var(--sh-font-mono);
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--sh-graphite);
  z-index: 2;
  pointer-events: none;
}
.canvas-empty {
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  font-size: 14px;
  text-align: center;
  max-width: 360px;
}
.sw-arrows {
  position: absolute;
  inset: 0;
  pointer-events: none;
  z-index: 0;
}
.sw-line { stroke: var(--sh-ink); stroke-width: 2; }
.sw-line-restricted { stroke: var(--sh-teal); stroke-width: 2; }
.sw-arr-head { fill: var(--sh-ink); }
.sw-arr-head-teal { fill: var(--sh-teal); }
.sw-arrow-label {
  font-family: var(--sh-font-mono);
  font-size: 9px;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  fill: var(--sh-teal);
  text-anchor: middle;
}
.sw-node { z-index: 1; }
.inspector .field { display: flex; flex-direction: column; gap: 4px; }
.inspector .field-label {
  font-family: var(--sh-font-mono);
  font-size: 10px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--sh-graphite);
}
.inspector .field-input {
  background: var(--sh-surface);
  border: 1.5px solid var(--sh-ink);
  border-radius: var(--sh-radius-sm);
  padding: 7px 10px;
  font-size: 13px;
  font-family: inherit;
  color: var(--sh-ink);
}
.advanced {
  margin-top: 10px;
  padding-top: 14px;
  border-top: 1.5px dashed rgba(31, 35, 48, 0.25);
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.spacer-v { flex: 1; }
.btn-danger { color: var(--sh-error, #c0392b); }
.worker-preview { padding: 24px; overflow-y: auto; }
.preview-banner {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 14px;
  background: var(--sh-marigold-8, rgba(242, 169, 59, 0.12));
  border: 1.5px solid var(--sh-marigold);
  border-radius: var(--sh-radius-sm);
  font-size: 13px;
  font-weight: 600;
}
.switchboard-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
  gap: var(--sh-space-3, 0.75rem);
}
.switchboard-button {
  display: flex;
  align-items: center;
  gap: var(--sh-space-2, 0.5rem);
  min-height: 44px;
  padding: var(--sh-space-3, 0.75rem) var(--sh-space-4, 1rem);
  background: var(--sh-marigold);
  color: var(--sh-ink);
  border: var(--sh-border, 2px solid var(--sh-ink));
  cursor: pointer;
  font-weight: 600;
  text-align: left;
}
.switchboard-button:hover { box-shadow: var(--sh-shadow-marigold); }
.nav-limit { margin-top: 6px; font-size: 12px; }
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
