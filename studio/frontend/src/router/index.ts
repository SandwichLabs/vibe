import { createRouter, createWebHashHistory, type RouteLocation } from 'vue-router'
import DashboardView from '../views/DashboardView.vue'
import LibraryView from '../views/LibraryView.vue'
import NotebookView from '../views/NotebookView.vue'
import TableBrowserView from '../views/TableBrowserView.vue'
import QueryEditorView from '../views/QueryEditorView.vue'
import ArtifactView from '../views/ArtifactView.vue'
import SkillsView from '../views/SkillsView.vue'
import ImportView from '../views/ImportView.vue'
import SettingsView from '../views/SettingsView.vue'
import AgentView from '../views/AgentView.vue'
import ConnectionEditorView from '../views/ConnectionEditorView.vue'
import { artifactPath, libraryPath, notebookPath } from '../lib/libraryRoutes'

function redirectNotebooks(to: RouteLocation) {
  const { new: n, type, ...rest } = to.query
  const query: Record<string, unknown> = { ...rest }
  if (n === '1') query.new = 'notebook'
  else if (n !== undefined) query.new = n
  query.type = type ?? (n === undefined ? 'notebook' : undefined)
  if (query.type === undefined) delete query.type
  return { path: '/library', query: query as RouteLocation['query'], hash: to.hash }
}

// #68: the Access application layer (forms, subforms, relationships UI,
// switchboard, macros, banded reports, App Mode) is gone. Studio is the
// only mode now, so there is no more mode-aware route guard or
// `meta.builderOnly` locking — every route below is reachable.
//
// #69: the page system (markdown + directives, web preview, page
// editor/runtime views) is gone too. Notebooks (#66) are the only
// authoring surface now.
const routes = [
  { path: '/', name: 'dashboard', component: DashboardView },
  // The Library (docs/plans/library.md L4): notebooks and artifacts in one list.
  { path: '/library', name: 'library', component: LibraryView },
  { path: '/library/notebook/:id', name: 'library-notebook', component: NotebookView },
  { path: '/library/artifact/:name', name: 'library-artifact', component: ArtifactView },
  // Old paths keep working (deep links, docs). Function-form redirects: a string
  // redirect drops the query, and ?new=1 must become ?new=notebook.
  { path: '/notebooks', redirect: redirectNotebooks },
  { path: '/notebooks/:id', redirect: (to: RouteLocation) => ({ path: notebookPath(String(to.params.id)), query: to.query, hash: to.hash }) },
  { path: '/artifacts', redirect: (to: RouteLocation) => ({ path: libraryPath(), query: to.query, hash: to.hash }) },
  { path: '/artifacts/:name', redirect: (to: RouteLocation) => ({ path: artifactPath(String(to.params.name)), query: to.query, hash: to.hash }) },
  // The rail replaces the Tables nav item (#117); the route stays for deep links
  // (/tables?table=prod_pg.public.orders).
  { path: '/tables', name: 'tables', component: TableBrowserView },
  // Connection editor (#117): a page, not a modal, so the rail stays visible.
  { path: '/connections/new', name: 'connection-new', component: ConnectionEditorView },
  { path: '/connections/:name', name: 'connection-edit', component: ConnectionEditorView },
  { path: '/queries', name: 'queries', component: QueryEditorView },
  // Data artifacts (#162): live, parameterised views stored as artifacts/<name>/.
  // Skills (#62) — domain context the agent loads on demand.
  { path: '/skills', name: 'skills', component: SkillsView },
  // Dedicated Agent wizard page (W0.5).
  { path: '/agent', name: 'agent', component: AgentView },
  { path: '/import', name: 'import', component: ImportView },
  { path: '/settings', name: 'settings', component: SettingsView },
]

export const router = createRouter({
  history: createWebHashHistory(),
  routes,
})
