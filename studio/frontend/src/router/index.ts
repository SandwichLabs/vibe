import { createRouter, createWebHashHistory } from 'vue-router'
import DashboardView from '../views/DashboardView.vue'
import NotebooksView from '../views/NotebooksView.vue'
import NotebookView from '../views/NotebookView.vue'
import TableBrowserView from '../views/TableBrowserView.vue'
import QueryEditorView from '../views/QueryEditorView.vue'
import SkillsView from '../views/SkillsView.vue'
import ImportView from '../views/ImportView.vue'
import SettingsView from '../views/SettingsView.vue'
import AgentView from '../views/AgentView.vue'

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
  { path: '/notebooks', name: 'notebooks', component: NotebooksView },
  { path: '/notebooks/:id', name: 'notebook', component: NotebookView },
  { path: '/tables', name: 'tables', component: TableBrowserView },
  { path: '/queries', name: 'queries', component: QueryEditorView },
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
