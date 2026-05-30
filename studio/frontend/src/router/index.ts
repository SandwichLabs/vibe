import { createRouter, createWebHashHistory } from 'vue-router'
import DashboardView from '../views/DashboardView.vue'
import PagesView from '../views/PagesView.vue'
import PageEditorView from '../views/PageEditorView.vue'
import TableBrowserView from '../views/TableBrowserView.vue'
import RelationshipsView from '../views/RelationshipsView.vue'
import SchemaDesignerView from '../views/SchemaDesignerView.vue'
import FormView from '../views/FormView.vue'
import ReportView from '../views/ReportView.vue'
import QueryEditorView from '../views/QueryEditorView.vue'
import ImportView from '../views/ImportView.vue'
import SettingsView from '../views/SettingsView.vue'

// Phase 4 / P4.6 — builder-only routes are locked in App Mode. Routes flagged
// `meta.builderOnly` expose schema/SQL/destructive surfaces and must be
// unreachable when the live app mode is "app"; the mode-aware guard
// (router.beforeEach below) redirects them to the switchboard/dashboard home.
const routes = [
  { path: '/', name: 'dashboard', component: DashboardView },
  { path: '/pages', name: 'pages', component: PagesView, meta: { builderOnly: true } },
  { path: '/pages/:id/edit', name: 'page-editor', component: PageEditorView, meta: { builderOnly: true } },
  { path: '/tables', name: 'tables', component: TableBrowserView, meta: { builderOnly: true } },
  { path: '/relationships', name: 'relationships', component: RelationshipsView, meta: { builderOnly: true } },
  { path: '/schema', name: 'schema', component: SchemaDesignerView, meta: { builderOnly: true } },
  { path: '/forms', name: 'forms', component: FormView },
  { path: '/reports', name: 'reports', component: ReportView },
  { path: '/queries', name: 'queries', component: QueryEditorView, meta: { builderOnly: true } },
  { path: '/import', name: 'import', component: ImportView, meta: { builderOnly: true } },
  { path: '/settings', name: 'settings', component: SettingsView, meta: { builderOnly: true } },
]

export const router = createRouter({
  history: createWebHashHistory(),
  routes,
})

// cachedMode is the last-known live app mode. The guard refreshes it from
// GetAppConfig, and SetMode flows should call setCachedMode() so the guard
// stays in sync without an extra round-trip.
let cachedMode: string | null = null

export function setCachedMode(mode: string) {
  cachedMode = mode === 'app' ? 'app' : 'studio'
}

export function getCachedMode(): string | null {
  return cachedMode
}

// resolveMode returns the live app mode, preferring the cache and falling back
// to GetAppConfig. Any failure (e.g. no Wails runtime / no file open) is treated
// as Studio Mode so the builder is never accidentally locked out.
async function resolveMode(): Promise<string> {
  if (cachedMode) return cachedMode
  try {
    const cfg = await window.go.main.App.GetAppConfig()
    cachedMode = cfg?.mode === 'app' ? 'app' : 'studio'
  } catch {
    cachedMode = 'studio'
  }
  return cachedMode
}

// Route-locking guard (P4.6 acceptance: locked routes are unreachable in App
// Mode). In App Mode any builderOnly route is redirected to the switchboard
// home (dashboard); switchboard/forms/reports stay reachable.
router.beforeEach(async (to) => {
  const mode = await resolveMode()
  if (mode === 'app' && to.meta?.builderOnly) {
    return { name: 'dashboard' }
  }
  return true
})
