<script setup lang="ts">
import { ref, onMounted, computed, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { setCachedMode } from '../router/index'
import { useAppMode } from '../composables/useAppMode'
import Icon from './Icon.vue'
import AgentPanel from './AgentPanel.vue'
import { destinationFor, iconFor, type NavItem } from '../lib/nav'

// Agent wizard panel (W0) — a slide-in chat available across Studio Mode screens.
const agentOpen = ref(false)

// The AppShell is the single place that reads/flips the live app mode. It keeps
// THREE consumers in sync: this chrome (appConfig), the router guard
// (setCachedMode), and the shared useAppMode composable that App-Mode views
// (AppShellView switchboard runtime, ReportView Edit-SQL gate) read from. Before
// this wiring the composable defaulted to "studio" forever, so the runtime
// switchboard never rendered its buttons even in App Mode.
const { setMode: setComposableMode } = useAppMode()

const emit = defineEmits<{ 'file-closed': [] }>()
const route = useRoute()
const router = useRouter()

const fileInfo = ref<FileInfo | null>(null)
const saving = ref(false)
const errorMessage = ref('')

// Phase 4 / P4.6 — mode-aware chrome. The live app mode ("studio" | "app") is
// read from GetAppConfig on mount and flipped at runtime via SetMode. App Mode
// hides the entire builder nav and re-skins the chrome (marigold, locked); the
// return to Studio Mode is gated behind an inline unlock entrypoint (P4.4).
const appConfig = ref<AppConfig | null>(null)
const mode = computed(() => appConfig.value?.mode === 'app' ? 'app' : 'studio')
const isAppMode = computed(() => mode.value === 'app')
// Brand falls back to the open file's title when no app_title is configured —
// an HVAC Ops .vibe should read "HVAC Ops" in App Mode, not "Untitled App".
const appTitle = computed(() => appConfig.value?.app_title || fileInfo.value?.title || 'Untitled App')
const appInitial = computed(() => (appTitle.value.trim().charAt(0) || 'A').toUpperCase())
const showUnlock = ref(false)
const unlockPassphrase = ref('')

// App-Mode top tab strip (W1) — the locked shell exposes the switchboard's
// top-level nav items as tabs (no sidebar). Loaded only in App Mode; the route
// mapping is shared with the runtime switchboard via lib/nav.ts so a tab and its
// switchboard button always go to the same place.
const appTabs = ref<NavItem[]>([])
async function loadAppTabs() {
  try {
    const items = ((await window.go.main.App.ListNav()) || []) as NavItem[]
    appTabs.value = items
      .filter((n) => n.parent_id === '')
      .slice()
      .sort((a, b) => a.sort_order - b.sort_order)
  } catch {
    appTabs.value = []
  }
}

function isTabActive(item: NavItem): boolean {
  const dest = destinationFor(item)
  if (!dest) return false
  if (typeof dest.path === 'string' && route.path !== dest.path) return false
  const id = (dest.query as Record<string, unknown> | undefined)?.id
  return id == null || route.query?.id === id
}

function activateTab(item: NavItem) {
  const dest = destinationFor(item)
  // run_macro (and any non-navigating kind) has no direct route — fall back to
  // the switchboard home, which owns the macro-runner dispatch.
  router.push(dest ?? { name: 'switchboard' })
}

function goHome() {
  router.push({ name: 'switchboard' })
}
const homeActive = computed(() => route.name === 'switchboard')

const navItems = [
  { path: '/', label: 'Dashboard', icon: 'home' },
  { path: '/switchboard', label: 'Switchboard', icon: 'apps' },
  { path: '/pages', label: 'Pages', icon: 'fileText' },
  { path: '/tables', label: 'Tables', icon: 'database' },
  { path: '/relationships', label: 'Relationships', icon: 'gitMerge' },
  { path: '/schema', label: 'Schema', icon: 'columns' },
  { path: '/forms', label: 'Forms', icon: 'form' },
  { path: '/queries', label: 'Queries', icon: 'terminal' },
  { path: '/agent', label: 'Agent', icon: 'bot' },
  { path: '/macros', label: 'Macros', icon: 'play' },
  { path: '/import', label: 'Import', icon: 'download' },
  { path: '/settings', label: 'Settings', icon: 'settings' },
]

const fileName = computed(() => {
  if (!fileInfo.value?.path) return 'Untitled'
  return fileInfo.value.path.split('/').pop() || 'Untitled'
})

function isActive(path: string) {
  return route.path === path || (path !== '/' && route.path.startsWith(path))
}

async function loadFileInfo() {
  try {
    fileInfo.value = await window.go.main.App.GetFileInfo()
  } catch { /* ignore */ }
}

async function loadAppConfig() {
  try {
    appConfig.value = await window.go.main.App.GetAppConfig()
    // Keep the route-locking guard AND the shared composable in sync with the
    // live mode (P4.6 / P4.7).
    const live = appConfig.value?.mode === 'app' ? 'app' : 'studio'
    setCachedMode(live)
    setComposableMode(live)
    // The locked shell's tab strip is driven by the switchboard nav; load it
    // whenever we're (or land) in App Mode.
    if (live === 'app') await loadAppTabs()
  } catch { /* ignore — fall back to Studio Mode */ }
}

// enterAppMode flips the live mode to the locked-down App Mode. After flipping,
// if the current route is a builder-only surface, bounce to the switchboard so
// the guard's lock takes effect immediately.
async function enterAppMode() {
  try {
    await window.go.main.App.SetMode('app')
    setCachedMode('app')
    await loadAppConfig()
    // App Mode lands on the switchboard (the runtime home). If we were on a
    // builder-only surface the guard would bounce us anyway; navigate explicitly
    // so the end-user always sees the switchboard first.
    router.replace({ name: 'switchboard' })
  } catch (e: any) {
    errorMessage.value = 'Could not enter App Mode: ' + (e.message || e)
  }
}

// requestStudioMode does NOT flip directly — returning to Studio Mode routes
// through the unlock entrypoint (P4.4) so an end-user cannot escape the locked
// shell without the passphrase.
function requestStudioMode() {
  showUnlock.value = true
}

async function confirmUnlock() {
  try {
    await window.go.main.App.SetMode('studio')
    setCachedMode('studio')
    await loadAppConfig()
    showUnlock.value = false
    unlockPassphrase.value = ''
  } catch (e: any) {
    errorMessage.value = 'Unlock failed: ' + (e.message || e)
  }
}

function cancelUnlock() {
  showUnlock.value = false
  unlockPassphrase.value = ''
}

async function save() {
  saving.value = true
  try {
    await window.go.main.App.SaveFile()
  } catch (e: any) {
    errorMessage.value = 'Save failed: ' + (e.message || e)
  } finally {
    saving.value = false
  }
}

async function closeFile() {
  try {
    await window.go.main.App.CloseFile()
    emit('file-closed')
  } catch (e: any) {
    errorMessage.value = 'Close failed: ' + (e.message || e)
  }
}

onMounted(() => {
  loadFileInfo()
  loadAppConfig()
})

watch(errorMessage, (msg) => {
  if (msg) setTimeout(() => { errorMessage.value = '' }, 3000)
})
</script>

<template>
  <div class="app" :class="{ 'app-mode': isAppMode }">
    <!-- Top bar — App Mode renders the locked white/marigold chrome (W1):
         brand-block + locked tab strip + lock pill + role line. Studio Mode
         keeps the dark builder chrome. -->
    <div v-if="isAppMode" class="topbar app" data-testid="app-topbar" style="height: 64px;">
      <div class="brand-block">
        <span class="mark">{{ appInitial }}</span>
        <div>
          <div class="name">{{ appTitle }}</div>
          <div class="role">App user · Locked</div>
        </div>
      </div>
      <nav class="tabstrip" data-testid="app-tabstrip">
        <a
          class="tab"
          :class="{ on: homeActive }"
          data-testid="app-tab"
          role="button"
          tabindex="0"
          @click="goHome"
          @keyup.enter="goHome"
        >
          <Icon name="home" :size="15" /> Home
        </a>
        <a
          v-for="item in appTabs"
          :key="item.id"
          class="tab"
          :class="{ on: isTabActive(item) }"
          data-testid="app-tab"
          role="button"
          tabindex="0"
          @click="activateTab(item)"
          @keyup.enter="activateTab(item)"
        >
          <Icon :name="iconFor(item)" :size="15" /> {{ item.label }}
        </a>
      </nav>
      <div class="right">
        <span class="lockpill" data-testid="app-lockpill"><Icon name="lock" :size="14" /> Locked · Encrypted</span>
        <span class="avatar-sq" style="border-color: var(--sh-ink);">{{ appInitial }}</span>
        <button
          class="btn btn-app-exit"
          data-testid="app-exit"
          title="Exit App Mode (requires passphrase)"
          @click="requestStudioMode"
        >
          <Icon name="settings" :size="16" /> Exit
        </button>
      </div>
    </div>
    <div v-else class="topbar" style="height: 64px;">
      <div class="brand">
        <span class="mark">V</span>
        <span class="word">Vibe<span class="last">Studio</span></span>
      </div>
      <div class="topright" style="gap: 10px;">
        <span class="pill studio"><span class="dot studio"></span>Studio Mode</span>
        <span class="pill"><span class="dot ok"></span>Local · Encrypted</span>
        <button
          class="btn btn-inverted agent-toggle"
          data-testid="agent-toggle"
          @click="agentOpen = !agentOpen"
        >
          <Icon name="bot" :size="16" /> Agent
        </button>
        <span class="avatar-sq">VS</span>
      </div>
    </div>

    <!-- Sidebar (builder nav) — Studio Mode only. In App Mode it is removed from
         the DOM entirely and the grid collapses to a single column (W1); the
         locked shell navigates via the top tab strip instead. -->
    <aside v-if="!isAppMode" class="sidebar" style="display: flex; flex-direction: column;">
      <div class="side-section">Workspace</div>
      <router-link
        v-for="item in navItems"
        :key="item.path"
        :to="item.path"
        class="side-link"
        :class="{ active: isActive(item.path) }"
      >
        <Icon :name="item.icon" :size="18" />
        <span>{{ item.label }}</span>
      </router-link>

      <div style="flex: 1;"></div>

      <div class="nav-pill">
        <span class="label"><span class="dot"></span>Open file</span>
        <span class="url">{{ fileName }}</span>
      </div>

      <div style="padding: 0 16px 16px; display: flex; flex-direction: column; gap: 8px;">
        <!-- Mode toggle: Studio→App previews the locked shell. (App→Studio is in
             the App-Mode topbar and routes through the unlock entrypoint, P4.4.) -->
        <button
          @click="enterAppMode"
          class="btn btn-inverted"
          style="width: 100%; justify-content: center;"
        >
          <Icon name="lock" :size="16" />
          Switch to App Mode
        </button>

        <button @click="save" :disabled="saving" class="btn btn-primary" style="width: 100%; justify-content: center;">
          <Icon name="save" :size="16" />
          {{ saving ? 'Saving…' : 'Save' }}
        </button>
        <button @click="closeFile" class="btn btn-inverted" style="width: 100%; justify-content: center;">
          Close File
        </button>
      </div>
    </aside>

    <!-- Main content -->
    <main style="overflow: hidden; background: var(--sh-paper); min-width: 0;">
      <router-view />
    </main>

    <!-- Unlock entrypoint (P4.4) — required to return to Studio Mode -->
    <div v-if="showUnlock" class="unlock-prompt" data-testid="unlock-prompt">
      <div class="unlock-card card card-pad">
        <h3>Unlock Studio Mode</h3>
        <p>Enter the file passphrase to leave the locked App Mode.</p>
        <input
          v-model="unlockPassphrase"
          type="password"
          placeholder="Passphrase"
          class="input"
          @keyup.enter="confirmUnlock"
        />
        <div style="display: flex; gap: 8px; justify-content: flex-end;">
          <button @click="cancelUnlock" class="btn btn-inverted">Cancel</button>
          <button @click="confirmUnlock" class="btn btn-primary">Unlock</button>
        </div>
      </div>
    </div>

    <!-- Agent wizard slide-in (W0) — Studio Mode only; not rendered at all in
         App Mode so the locked-down end-user surface has no agent affordance. -->
    <AgentPanel v-if="!isAppMode" :open="agentOpen" @close="agentOpen = false" />

    <div v-if="errorMessage" class="error-toast">{{ errorMessage }}</div>
  </div>
</template>

<style scoped>
/* App Mode (W1): collapse the 240px sidebar gutter — the locked shell has no
   sidebar, so the grid becomes a single full-width column under the topbar.
   The scoped attribute selector outranks the global `.app` rule in kit.css. */
.app-mode {
  grid-template-columns: 1fr;
}

/* The top tab strip behaves as nav — make tabs feel clickable and keep their
   hit target tall (App-Mode 44/52px rule). */
.topbar.app .tabstrip .tab {
  cursor: pointer;
}
.topbar.app .tabstrip .tab:hover {
  background: var(--sh-paper-2);
}
.topbar.app .tabstrip .tab.on:hover {
  background: var(--sh-marigold);
}

/* Exit-to-Studio control in the locked topbar — compact, ink-outlined. */
.btn-app-exit {
  min-height: 40px;
  padding: 8px 14px;
}

.unlock-prompt {
  position: fixed;
  inset: 0;
  z-index: 1000;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(31, 35, 48, 0.45);
}
.unlock-card {
  width: 360px;
  max-width: 90vw;
  display: flex;
  flex-direction: column;
  gap: 12px;
  background: var(--sh-surface);
}
.unlock-card h3 {
  font-family: var(--sh-font-display);
  margin: 0;
}
.unlock-card p {
  margin: 0;
  color: var(--sh-graphite);
  font-size: 14px;
}
.unlock-card .input {
  width: 100%;
  padding: 8px 10px;
  border: 1.5px solid var(--sh-ink);
  border-radius: 3px;
  font-family: var(--sh-font-mono);
}
</style>
