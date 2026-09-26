import { createApp } from 'vue'
import App from './App.vue'
import { router } from './router'
import './style.css'
import { installSafeLinkHandling } from './lib/markdown'
import { installTheme } from './theme/useTheme'


// Rendered-markdown link safety (Skills, agent chat) — see lib/markdown.ts.
installSafeLinkHandling()

// Theme (Settings → Appearance): apply the saved theme before first paint so
// the app never flashes the default palette. bridge.css carries a static
// fallback for the instant before this resolves.
installTheme().finally(() => {
  createApp(App).use(router).mount('#app')
})
