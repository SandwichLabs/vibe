<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'

const router = useRouter()
const data = ref<DashboardData | null>(null)
const loading = ref(true)

function formatBytes(bytes: number): string {
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  return (bytes / (1024 * 1024)).toFixed(1) + ' MB'
}

async function load() {
  loading.value = true
  try {
    data.value = await window.go.main.App.GetDashboard()
  } catch { /* ignore */ }
  loading.value = false
}

onMounted(load)
</script>

<template>
  <div class="p-6 max-w-5xl">
    <h1 class="text-2xl font-bold mb-6">Dashboard</h1>

    <div v-if="loading" class="text-muted font-mono text-sm">Loading...</div>

    <template v-else-if="data">
      <div v-if="data.title" class="mb-6">
        <h2 class="text-xl font-bold">{{ data.title }}</h2>
        <p v-if="data.description" class="text-muted mt-1">{{ data.description }}</p>
      </div>

      <div class="grid grid-cols-2 md:grid-cols-4 gap-4 mb-8">
        <div class="card text-center p-4">
          <div class="text-3xl font-bold">{{ data.page_count }}</div>
          <div class="font-mono text-xs text-muted uppercase tracking-wide mt-1">Pages</div>
        </div>
        <div class="card text-center p-4">
          <div class="text-3xl font-bold">{{ data.table_count }}</div>
          <div class="font-mono text-xs text-muted uppercase tracking-wide mt-1">Tables</div>
        </div>
        <div class="card text-center p-4">
          <div class="text-3xl font-bold">{{ data.query_count }}</div>
          <div class="font-mono text-xs text-muted uppercase tracking-wide mt-1">Queries</div>
        </div>
        <div class="card text-center p-4">
          <div class="text-3xl font-bold">{{ formatBytes(data.file_size) }}</div>
          <div class="font-mono text-xs text-muted uppercase tracking-wide mt-1">File Size</div>
        </div>
      </div>

      <div class="mb-8">
        <h3 class="text-lg font-bold mb-3">Quick Actions</h3>
        <div class="flex gap-3">
          <button @click="router.push('/pages?new=1')" class="btn btn-primary">New Page</button>
          <button @click="router.push('/import')" class="btn">Import CSV</button>
          <button @click="router.push('/queries')" class="btn">New Query</button>
        </div>
      </div>

      <div v-if="data.created_at || data.updated_at" class="font-mono text-xs text-muted space-y-1">
        <div v-if="data.created_at">Created: {{ data.created_at }}</div>
        <div v-if="data.updated_at">Updated: {{ data.updated_at }}</div>
      </div>
    </template>
  </div>
</template>
