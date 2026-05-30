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

interface NavItem {
  id: string
  parent_id: string
  label: string
  action_kind: string
  target_id: string
  sort_order: number
}

interface NavTarget {
  kind: string
  id: string
  label: string
}

// Access constraint: at most 8 items per switchboard page.
const MAX_PER_PAGE = 8

const { isAppMode, isStudio } = useAppMode()

// Reach the injected router (tests inject it as the `$router` mock; at runtime
// vue-router provides the same proxy property).
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

async function save() {
  errorMessage.value = ''
  try {
    await window.go.main.App.SaveNav(navItems.value.map((n) => ({ ...n })))
  } catch (err) {
    errorMessage.value = `Failed to save navigation: ${err}`
  }
}

// Map a nav item to a router destination. JSON of the destination carries both
// the route segment (forms/reports/pages) and the bound target id.
function destinationFor(item: NavItem): unknown | null {
  switch (item.action_kind) {
    case 'open_form':
      return { path: `/forms`, query: { id: item.target_id } }
    case 'open_report':
      return { path: `/reports`, query: { id: item.target_id } }
    case 'open_page':
      return { path: `/pages/${item.target_id}/edit`, query: { id: item.target_id } }
    case 'open_switchboard':
      return { path: `/`, query: { switchboard: item.target_id } }
    default:
      return null
  }
}

function activate(item: NavItem) {
  const dest = destinationFor(item)
  if (!dest) return
  router()?.push(dest)
}

onMounted(async () => {
  await load()

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
          <Icon name="square" :size="20" />
          <span class="switchboard-button-label">{{ item.label }}</span>
        </button>
      </div>
      <p v-if="!pageItems.length" class="sh-muted">No navigation items defined.</p>
    </div>

    <!-- Studio Mode: switchboard builder -->
    <div v-else-if="isStudio" class="switchboard-builder">
      <header class="builder-header">
        <h2 class="sh-h2">Switchboard</h2>
        <button data-testid="save-nav" class="sh-btn" @click="save">Save</button>
      </header>

      <ul class="nav-item-list">
        <li
          v-for="item in pageItems"
          :key="item.id"
          data-testid="nav-item"
          class="nav-item sh-card"
        >
          <span class="nav-item-label">{{ item.label }}</span>
          <span class="nav-item-action sh-muted">{{ item.action_kind }}</span>
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
  justify-content: space-between;
  padding: var(--sh-space-2, 0.5rem) var(--sh-space-3, 0.75rem);
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
