<script setup lang="ts">
// AppShell — the workspace shell (#111, SPEC 04): top bar with the workspace
// switcher and the agent-trust pill, the nav sidebar with the "Saved as you go"
// footer, a banner slot for the read-only state, and the routed view. Save,
// Revert, Close file and the dirty pill are gone: notebooks, queries and skills
// save as files, and data writes go straight to the connection.
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useRoute } from 'vue-router'
import { WindowSetTitle } from '../wailsjs/runtime/runtime'
import Icon from './Icon.vue'
import AgentPanel from './AgentPanel.vue'
import WorkspaceSwitcher from './WorkspaceSwitcher.vue'
import WorkspaceBanner from './WorkspaceBanner.vue'
import McpApprovals from './McpApprovals.vue'
import ConnectionRail from './ConnectionRail.vue'
import ConnectionDialogs from './ConnectionDialogs.vue'
import { useTheme } from '../theme/useTheme'
import { shortcutLabel } from '../composables/useModal'
import { workspaceInfo } from '../composables/useWorkspace'
import { useAgentChat } from '../composables/useAgentChat'
import { hasWailsRuntime } from '../lib/wails'

const theme = useTheme()

// Agent wizard panel — a slide-in chat available across every screen.
const agentOpen = ref(false)
// A turn keeps running when its view is left (#2): say so on the Agent entries.
const { sending: agentBusy } = useAgentChat()
const route = useRoute()
const errorMessage = ref('')

const info = computed(() => workspaceInfo.value)

// Home, then the content that lives in files, then the data views. The
// Connections rail below the nav replaces "Tables" (#117); /tables stays as a
// route for deep links.
const navItems = [
  { path: '/', label: 'Home', icon: 'home' },
  { path: '/notebooks', label: 'Notebooks', icon: 'layers' },
  { path: '/queries', label: 'Queries', icon: 'terminal' },
  { path: '/skills', label: 'Skills', icon: 'bookOpen' },
  { path: '/agent', label: 'Agent', icon: 'bot' },
  { path: '/import', label: 'Import', icon: 'download' },
  { path: '/settings', label: 'Settings', icon: 'settings' },
]

function isActive(path: string) {
  return route.path === path || (path !== '/' && route.path.startsWith(path))
}

// The agent-trust pill is about the agent only; read-only is explained by the
// banner, not here (SPEC 07 C).
const pill = computed(() => {
  const p = info.value?.policy
  if (!p) return { text: '', dot: 'ok' }
  if (info.value?.readOnly || p.sandbox === 'read-only') return { text: 'Agent can only read here', dot: 'ok' }
  if (p.askBeforeWrites) return { text: 'Agent asks before changes', dot: 'ok' }
  return { text: 'Agent changes data without asking', dot: 'warn' }
})

const folderParts = computed(() => {
  const root = info.value?.root ?? ''
  const cut = Math.max(root.lastIndexOf('/'), root.lastIndexOf('\\'))
  return cut < 0 ? { dir: '', leaf: root } : { dir: root.slice(0, cut + 1), leaf: root.slice(cut + 1) }
})

// Only the branch (read from .git/HEAD by the backend): Studio never runs git.
const gitLine = computed(() => (info.value?.isGit ? info.value.gitBranch || 'detached' : ''))

// Views (e.g. the Query Explorer's "Ask agent" button) open the wizard panel
// via this window event — the shell owns the panel, so this is the one
// cross-component trigger.
function openAgentFromEvent() {
  agentOpen.value = true
}


// Ctrl/Cmd+J toggles the agent drawer; Escape closes it when nothing else (a
// modal, an editor) claims the key first. Workspace shortcuts (O, N, Shift+O,
// F5) live in App.vue so they also work on the launcher.
function onGlobalKeydown(e: KeyboardEvent) {
  const mod = e.metaKey || e.ctrlKey
  if (mod && !e.shiftKey && !e.altKey && e.key.toLowerCase() === 'j') {
    e.preventDefault()
    agentOpen.value = !agentOpen.value
    return
  }
  if (e.key === 'Escape' && agentOpen.value && !e.defaultPrevented) {
    agentOpen.value = false
  }
}

const unsubs: Array<() => void> = []

onMounted(() => {
  window.addEventListener('vibe:open-agent', openAgentFromEvent)
  window.addEventListener('keydown', onGlobalKeydown)
})

onUnmounted(() => {
  window.removeEventListener('vibe:open-agent', openAgentFromEvent)
  window.removeEventListener('keydown', onGlobalKeydown)
  unsubs.splice(0).forEach((u) => u())
  if (hasWailsRuntime()) WindowSetTitle('Vibe Studio')
})

watch(errorMessage, (msg) => {
  if (msg) setTimeout(() => { errorMessage.value = '' }, 4000)
})

watch(() => info.value?.name, (name) => {
  if (!hasWailsRuntime()) return
  WindowSetTitle(name ? `${name} — Vibe Studio` : 'Vibe Studio')
}, { immediate: true })
</script>

<template>
  <div class="app has-switcher has-rail">
    <div class="topbar" style="height: 64px;">
      <div class="brand"><span class="mark mark-only" aria-hidden="true">V</span></div>
      <WorkspaceSwitcher @error="(m: string) => (errorMessage = m)" />
      <div class="topright" style="gap: 10px;">
        <span v-if="pill.text" class="pill" data-testid="agent-pill"><span class="dot" :class="pill.dot"></span>{{ pill.text }}</span>
        <button
          class="btn btn-inverted agent-toggle"
          data-testid="agent-toggle"
          :aria-expanded="agentOpen"
          aria-controls="agent-panel"
          :title="`Toggle agent · ${shortcutLabel('J')}${agentBusy ? ' · a turn is running' : ''}`"
          @click="agentOpen = !agentOpen"
        >
          <Icon name="bot" :size="16" /> Agent
          <i v-if="agentBusy" class="agent-busy" data-testid="agent-busy-toggle" aria-hidden="true"></i>
        </button>
        <button
          class="theme-toggle"
          data-testid="theme-toggle"
          :aria-label="theme.activeTheme.value.type === 'dark' ? 'Switch to light theme' : 'Switch to dark theme'"
          :title="`${theme.activeTheme.value.name} · click to switch`"
          @click="theme.toggleDark()"
        >
          <Icon :name="theme.activeTheme.value.type === 'dark' ? 'sun' : 'moon'" :size="16" />
        </button>
      </div>
    </div>

    <!-- Sidebar (nav). -->
    <aside class="sidebar with-rail">
      <nav class="navblock" aria-label="Workspace">
        <div class="side-section">Workspace</div>
        <router-link
          v-for="item in navItems"
          :key="item.path"
          :to="item.path"
          class="side-link"
          :class="{ active: isActive(item.path) }"
          :aria-current="isActive(item.path) ? 'page' : undefined"
          :aria-label="item.path === '/agent' && agentBusy ? 'Agent, a turn is running' : item.label"
          :title="item.path === '/agent' && agentBusy ? 'Agent: a turn is running' : item.label"
        >
          <Icon :name="item.icon" :size="18" />
          <span>{{ item.label }}</span>
          <i v-if="item.path === '/agent' && agentBusy" class="agent-busy" data-testid="agent-busy-nav" aria-hidden="true"></i>
        </router-link>
      </nav>

      <ConnectionRail @error="(m: string) => (errorMessage = m)" />

      <div v-if="info" class="nav-pill" style="margin: 10px 12px 12px;" data-testid="folder-pill">
        <span class="label"><span class="dot"></span>Saved as you go</span>
        <span class="url" :title="info.root">{{ folderParts.dir }}<span class="acc">{{ folderParts.leaf }}</span></span>
        <span v-if="gitLine" class="git" data-testid="git-line"><Icon name="gitBranch" :size="12" /> {{ gitLine }}</span>
      </div>
    </aside>

    <!-- Main content -->
    <main style="overflow: hidden; background: var(--sh-paper); min-width: 0; display: flex; flex-direction: column;">
      <WorkspaceBanner />
      <div style="flex: 1; min-height: 0; overflow: hidden;">
        <router-view :key="info?.sessionId ?? 'none'" />
      </div>
    </main>

    <AgentPanel :open="agentOpen" @close="agentOpen = false" />
    <McpApprovals />
    <ConnectionDialogs />

    <div v-if="errorMessage" class="error-toast" role="alert">{{ errorMessage }}</div>
  </div>
</template>

<style scoped>
.theme-toggle {
  width: 32px; height: 32px;
  display: inline-flex; align-items: center; justify-content: center;
  background: transparent;
  color: var(--vibe-chrome-fg);
  border: 1.5px solid var(--vibe-chrome-fg);
  border-radius: var(--sh-radius-sm);
  cursor: pointer;
}
.theme-toggle:hover { background: color-mix(in srgb, var(--vibe-chrome-fg) 12%, transparent); }
/* In-flight agent turn marker: a dot, not text, so it survives the collapsed sidebar. */
.agent-busy {
  display: inline-block;
  width: 8px; height: 8px;
  margin-left: 6px;
  border-radius: 50%;
  background: var(--vibe-accent);
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--vibe-accent) 30%, transparent);
  animation: agent-busy-pulse 1.4s ease-in-out infinite;
}
@keyframes agent-busy-pulse { 50% { opacity: 0.35; } }
@media (prefers-reduced-motion: reduce) { .agent-busy { animation: none; } }
</style>
