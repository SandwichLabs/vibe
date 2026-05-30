<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import Icon from '../components/Icon.vue'

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
  <div style="height: 100%; overflow-y: auto; padding: 32px;">
    <div class="page-head">
      <div>
        <div class="crumb">Studio <span class="sep">/</span> Home</div>
        <h1>{{ data?.title || 'Dashboard' }}</h1>
      </div>
      <div class="row">
        <button class="btn" @click="router.push('/import')">
          <Icon name="download" :size="16" /> Import CSV
        </button>
        <button class="btn btn-primary" @click="router.push('/pages?new=1')">
          <Icon name="plus" :size="16" /> New page
        </button>
      </div>
    </div>

    <p v-if="data?.description" class="sh-lead" style="margin-bottom: 24px; max-width: 720px;">
      {{ data.description }}
    </p>

    <div v-if="loading" class="sh-mono" style="color: var(--sh-graphite);">Loading…</div>

    <template v-else-if="data">
      <div class="stats">
        <div class="stat">
          <div class="label">Pages</div>
          <div class="num">{{ data.page_count }}</div>
        </div>
        <div class="stat">
          <div class="label">Tables</div>
          <div class="num">{{ data.table_count }}</div>
        </div>
        <div class="stat">
          <div class="label">Queries</div>
          <div class="num">{{ data.query_count }}</div>
        </div>
        <div class="stat">
          <div class="label">File size</div>
          <div class="num">{{ formatBytes(data.file_size) }}</div>
        </div>
      </div>

      <div class="sec-title">
        <h2>Quick actions</h2>
        <span class="label">Jump back in</span>
      </div>
      <div style="display: grid; grid-template-columns: repeat(3, 1fr); gap: 16px;">
        <div class="card" style="padding: 20px 20px 18px 28px; cursor: pointer; border-left: 6px solid var(--sh-teal);" @click="router.push('/pages')">
          <div class="row" style="justify-content: space-between; align-items: flex-start;">
            <div style="width: 40px; height: 40px; border: 1.5px solid var(--sh-ink); border-radius: var(--sh-radius-sm); background: var(--sh-paper-2); display: inline-flex; align-items: center; justify-content: center;">
              <Icon name="fileText" :size="22" />
            </div>
          </div>
          <div style="font-family: var(--sh-font-display); font-weight: 600; font-size: 20px; letter-spacing: -0.015em; margin-top: 12px;">Pages</div>
          <div class="sh-meta" style="margin-top: 4px;">Author markdown & HTML pages</div>
        </div>
        <div class="card" style="padding: 20px 20px 18px 28px; cursor: pointer; border-left: 6px solid var(--sh-subj-science);" @click="router.push('/tables')">
          <div class="row" style="justify-content: space-between; align-items: flex-start;">
            <div style="width: 40px; height: 40px; border: 1.5px solid var(--sh-ink); border-radius: var(--sh-radius-sm); background: var(--sh-paper-2); display: inline-flex; align-items: center; justify-content: center;">
              <Icon name="database" :size="22" />
            </div>
          </div>
          <div style="font-family: var(--sh-font-display); font-weight: 600; font-size: 20px; letter-spacing: -0.015em; margin-top: 12px;">Tables</div>
          <div class="sh-meta" style="margin-top: 4px;">Browse & edit your data</div>
        </div>
        <div class="card" style="padding: 20px 20px 18px 28px; cursor: pointer; border-left: 6px solid var(--sh-subj-math);" @click="router.push('/queries')">
          <div class="row" style="justify-content: space-between; align-items: flex-start;">
            <div style="width: 40px; height: 40px; border: 1.5px solid var(--sh-ink); border-radius: var(--sh-radius-sm); background: var(--sh-paper-2); display: inline-flex; align-items: center; justify-content: center;">
              <Icon name="terminal" :size="22" />
            </div>
          </div>
          <div style="font-family: var(--sh-font-display); font-weight: 600; font-size: 20px; letter-spacing: -0.015em; margin-top: 12px;">Queries</div>
          <div class="sh-meta" style="margin-top: 4px;">Run SQL on DuckDB & SQLite</div>
        </div>
      </div>

      <div v-if="data.created_at || data.updated_at" class="card" style="margin-top: 24px;">
        <div class="card-head">
          <h3 class="card-title">File details</h3>
          <span class="sh-label">Metadata</span>
        </div>
        <div style="padding: 6px 22px 14px;">
          <div v-if="data.created_at" class="row" style="justify-content: space-between; padding: 6px 0;">
            <span class="sh-label">Created</span>
            <span class="sh-mono">{{ data.created_at }}</span>
          </div>
          <div v-if="data.updated_at" class="row" style="justify-content: space-between; padding: 6px 0;">
            <span class="sh-label">Updated</span>
            <span class="sh-mono">{{ data.updated_at }}</span>
          </div>
        </div>
      </div>
    </template>
  </div>
</template>
