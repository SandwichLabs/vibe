/// <reference types="vitest/config" />
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
import type { Plugin } from 'vite'
import { injectFrameCsp, studioOrigins } from './scripts/frame-csp.mjs'

// Emits the narrowed frame-src meta into index.html for the window's origin
// (scripts/frame-csp.mjs says which, and why).
function frameCsp(): Plugin {
  let command: 'serve' | 'build' = 'build'
  return {
    name: 'vibe-frame-csp',
    configResolved(c) { command = c.command },
    transformIndexHtml: { order: 'pre', handler: (html) => injectFrameCsp(html, studioOrigins(command, process.env, process.platform)) },
  }
}

export default defineConfig({
  plugins: [vue(), tailwindcss(), frameCsp()],
  build: {
    outDir: 'dist',
  },
  test: {
    environment: 'jsdom',
    globals: true,
    include: ['src/**/*.{test,spec}.ts'],
  },
})
