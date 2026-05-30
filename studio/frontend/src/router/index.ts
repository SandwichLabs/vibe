import { createRouter, createWebHashHistory } from 'vue-router'
import DashboardView from '../views/DashboardView.vue'
import PagesView from '../views/PagesView.vue'
import PageEditorView from '../views/PageEditorView.vue'
import TableBrowserView from '../views/TableBrowserView.vue'
import RelationshipsView from '../views/RelationshipsView.vue'
import FormView from '../views/FormView.vue'
import QueryEditorView from '../views/QueryEditorView.vue'
import ImportView from '../views/ImportView.vue'
import SettingsView from '../views/SettingsView.vue'

const routes = [
  { path: '/', name: 'dashboard', component: DashboardView },
  { path: '/pages', name: 'pages', component: PagesView },
  { path: '/pages/:id/edit', name: 'page-editor', component: PageEditorView },
  { path: '/tables', name: 'tables', component: TableBrowserView },
  { path: '/relationships', name: 'relationships', component: RelationshipsView },
  { path: '/forms', name: 'forms', component: FormView },
  { path: '/queries', name: 'queries', component: QueryEditorView },
  { path: '/import', name: 'import', component: ImportView },
  { path: '/settings', name: 'settings', component: SettingsView },
]

export const router = createRouter({
  history: createWebHashHistory(),
  routes,
})
