import { createApp } from 'vue'
import App from './App.vue'
import { router } from './router'
import './style.css'

// Register Chart.js globally so inline <script> tags in rendered pages can use it.
import Chart from 'chart.js/auto'
;(window as any).Chart = Chart

createApp(App).use(router).mount('#app')
