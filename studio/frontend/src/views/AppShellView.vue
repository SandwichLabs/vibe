<script setup lang="ts">
// P4.7 AppShellView — the Switchboard (parity-plan §4.2; mockups #5 sw-main
// builder / #6 app-desktop runtime).
//
// One view, two responsibilities driven by the live app mode:
//
//   (a) Studio Mode — a builder canvas to compose the switchboard: list the nav
//       items for the current switchboard page, pick an action + target from
//       ListNavTargets, add items (capped at 8 per page — the Access
//       constraint), and persist the whole structure via SaveNav.
//
//   (b) App Mode — the runtime switchboard: render each nav item as a large
//       44px-hit-target marigold button that routes to the bound form / report
//       / page, honoring startup_object as the initial landing screen.
import { ref, computed, onMounted, getCurrentInstance } from 'vue'
import Icon from '../components/Icon.vue'
import { useAppMode } from '../composables/useAppMode'
import { useMacroIntents, type MacroIntent } from '../composables/useMacroIntents'
import { destinationFor, type NavItem } from '../lib/nav'

interface NavTarget {
  kind: string
  id: string
  label: string
}

// Access constraint: at most 8 items per switchboard page.
const MAX_PER_PAGE = 8

const { isAppMode, isStudio } = useAppMode()

// Reach the router. At runtime vue-router exposes it on the instance proxy as
// `$router`; the unit spec injects the same `$router` mock so navigation is
// observable without standing up a full router. We deliberately use the proxy
// property (not useRouter()) so the presentational unit test stays lightweight.
const instance = getCurrentInstance()
function router(): { push: (to: unknown) => unknown } | undefined {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  return (instance?.proxy as any)?.$router
}

const navItems = ref<NavItem[]>([])
const targets = ref<NavTarget[]>([])
const errorMessage = ref('')

// Builder state: the currently-edited switchboard page (root = '') and the
// new-item draft.
const currentPage = ref('')
const selectedTarget = ref('')
const newLabel = ref('')

// Items belonging to the switchboard page being edited / rendered.
const pageItems = computed(() =>
  navItems.value
    .filter((n) => n.parent_id === currentPage.value)
    .slice()
    .sort((a, b) => a.sort_order - b.sort_order),
)

const atLimit = computed(() => pageItems.value.length >= MAX_PER_PAGE)

function targetFor(id: string): NavTarget | undefined {
  return targets.value.find((t) => t.id === id)
}

async function load() {
  errorMessage.value = ''
  try {
    navItems.value = (await window.go.main.App.ListNav()) || []
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

function addItem() {
  if (atLimit.value) return
  const tgt = targetFor(selectedTarget.value)
  if (!tgt) return
  const label = newLabel.value.trim() || tgt.label
  navItems.value.push({
    id: `nav_${tgt.id}_${Date.now()}`,
    parent_id: currentPage.value,
    label,
    action_kind: tgt.kind,
    target_id: tgt.id,
    sort_order: pageItems.value.length,
  })
  selectedTarget.value = ''
  newLabel.value = ''
}

// removeItem deletes a nav item from the working set and re-packs sort_order so
// the remaining items stay 0..n-1 contiguous.
function removeItem(item: NavItem) {
  navItems.value = navItems.value.filter((n) => n.id !== item.id)
  renumber()
}

// startEdit/commitEdit drive inline relabeling of a nav item's button caption.
const editingId = ref<string | null>(null)
const editLabel = ref('')
function startEdit(item: NavItem) {
  editingId.value = item.id
  editLabel.value = item.label
}
function commitEdit(item: NavItem) {
  const next = editLabel.value.trim()
  if (next) item.label = next
  editingId.value = null
  editLabel.value = ''
}

// move shifts an item up (-1) or down (+1) within its switchboard page and
// rewrites sort_order on every sibling so the persisted order matches the UI.
function move(item: NavItem, delta: number) {
  const ordered = pageItems.value
  const idx = ordered.findIndex((n) => n.id === item.id)
  const swapWith = idx + delta
  if (idx < 0 || swapWith < 0 || swapWith >= ordered.length) return
  // Swap the sort_order of the two siblings, then renumber to be safe.
  const a = ordered[idx]
  const b = ordered[swapWith]
  const tmp = a.sort_order
  a.sort_order = b.sort_order
  b.sort_order = tmp
  renumber()
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

async function save() {
  errorMessage.value = ''
  try {
    await window.go.main.App.SaveNav(navItems.value.map((n) => ({ ...n })))
  } catch (err) {
    errorMessage.value = `Failed to save navigation: ${err}`
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
</script>

<template>
  <section class="app-shell-view">
    <!-- App Mode: runtime switchboard -->
    <div v-if="isAppMode" class="switchboard-runtime">
      <div class="switchboard-grid">
        <button
          v-for="item in pageItems"
          :key="item.id"
          data-testid="switchboard-button"
          class="switchboard-button sh-card"
          style="min-height: 44px; background: var(--sh-marigold);"
          @click="activate(item)"
        >
          <Icon name="apps" :size="20" />
          <span class="switchboard-button-label">{{ item.label }}</span>
        </button>
      </div>
      <p v-if="!pageItems.length" class="sh-muted">No navigation items defined.</p>

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

    <!-- Studio Mode: switchboard builder -->
    <div v-else-if="isStudio" class="switchboard-builder">
      <header class="builder-header">
        <h2 class="sh-h2">Switchboard</h2>
        <button data-testid="save-nav" class="sh-btn" @click="save">Save</button>
      </header>

      <ul class="nav-item-list">
        <li
          v-for="(item, idx) in pageItems"
          :key="item.id"
          data-testid="nav-item"
          class="nav-item sh-card"
        >
          <span class="nav-item-handle sh-muted">{{ item.sort_order + 1 }}.</span>
          <template v-if="editingId === item.id">
            <input
              v-model="editLabel"
              data-testid="edit-item-label"
              class="sh-input nav-item-edit"
              type="text"
              @keyup.enter="commitEdit(item)"
            />
            <button data-testid="commit-edit" class="sh-btn sh-btn-sm" @click="commitEdit(item)">Done</button>
          </template>
          <template v-else>
            <span class="nav-item-label">{{ item.label }}</span>
            <span class="nav-item-action sh-muted">{{ item.action_kind }}</span>
            <span class="nav-item-spacer"></span>
            <button
              data-testid="move-up"
              class="sh-btn sh-btn-sm"
              :disabled="idx === 0"
              title="Move up"
              aria-label="Move up"
              @click="move(item, -1)"
            >
              <Icon name="arrowLeft" :size="14" />
            </button>
            <button
              data-testid="move-down"
              class="sh-btn sh-btn-sm"
              :disabled="idx === pageItems.length - 1"
              title="Move down"
              aria-label="Move down"
              @click="move(item, 1)"
            >
              <Icon name="arrowRight" :size="14" />
            </button>
            <button data-testid="edit-item" class="sh-btn sh-btn-sm" title="Rename" @click="startEdit(item)">
              <Icon name="edit" :size="14" />
            </button>
            <button data-testid="remove-item" class="sh-btn sh-btn-sm" title="Remove" @click="removeItem(item)">
              <Icon name="trash" :size="14" />
            </button>
          </template>
        </li>
      </ul>

      <div class="builder-add">
        <select
          v-model="selectedTarget"
          data-testid="target-picker"
          class="sh-input"
        >
          <option value="">Select a target…</option>
          <option v-for="t in targets" :key="t.id" :value="t.id">{{ t.label }}</option>
        </select>
        <input
          v-model="newLabel"
          data-testid="new-item-label"
          class="sh-input"
          type="text"
          placeholder="Button label (optional)"
        />
        <button
          data-testid="add-nav-item"
          class="sh-btn"
          :disabled="atLimit"
          @click="addItem"
        >
          Add item
        </button>
      </div>

      <p v-if="atLimit" data-testid="nav-limit" class="nav-limit sh-muted">
        This switchboard page is full (max {{ MAX_PER_PAGE }} items). Create a sub-page to add more.
      </p>

      <p v-if="errorMessage" class="sh-error">{{ errorMessage }}</p>
    </div>
  </section>
</template>

<style scoped>
.app-shell-view {
  padding: var(--sh-space-4, 1rem);
}
.builder-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: var(--sh-space-3, 0.75rem);
}
.nav-item-list {
  list-style: none;
  margin: 0 0 var(--sh-space-3, 0.75rem);
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: var(--sh-space-2, 0.5rem);
}
.nav-item {
  display: flex;
  align-items: center;
  gap: var(--sh-space-2, 0.5rem);
  padding: var(--sh-space-2, 0.5rem) var(--sh-space-3, 0.75rem);
}
.nav-item-spacer {
  flex: 1;
}
.nav-item-edit {
  flex: 1;
}
.sh-btn-sm {
  padding: 2px 6px;
}
.builder-add {
  display: flex;
  gap: var(--sh-space-2, 0.5rem);
  align-items: center;
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
.switchboard-button:hover {
  box-shadow: var(--sh-shadow-marigold);
}
.nav-limit {
  margin-top: var(--sh-space-2, 0.5rem);
}
</style>
