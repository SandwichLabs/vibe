import { createApp } from 'vue'
import App from './App.vue'
import { router } from './router'
import './style.css'
import { installSafeLinkHandling } from './lib/markdown'


// Rendered-markdown link safety (Skills, agent chat) — see lib/markdown.ts.
installSafeLinkHandling()

createApp(App).use(router).mount('#app')
