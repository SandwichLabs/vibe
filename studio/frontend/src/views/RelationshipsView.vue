<script setup lang="ts">
// P2.8 RelationshipsView — the Schema Builder relationship view. Renders stored
// foreign-key relationships as parent -> child with the cascade (ON DELETE)
// label, lets the user declare a new FK via AddForeignKey, and remove one via
// RemoveForeignKey. Translates the #3 schema-main mockup into Vue + SchoolHouse.
import { ref, onMounted } from 'vue'
import Icon from '../components/Icon.vue'

interface Relationship {
  id: string
  parent_table: string
  parent_column: string
  child_table: string
  child_column: string
  on_delete: string
  on_update?: string
  enforce_integrity?: boolean
  created_at?: string
}

const relationships = ref<Relationship[]>([])
const tables = ref<string[]>([])
// Column lists keyed by table name, resolved lazily from DescribeTable.
const columnsByTable = ref<Record<string, { name: string; type: string; not_null: boolean; pk: boolean }[]>>({})
const loading = ref(true)
const errorMessage = ref('')

// Add-relationship form state.
const showAddForm = ref(false)
const form = ref({
  parent_table: '',
  parent_column: '',
  child_table: '',
  child_column: '',
  on_delete: 'NO ACTION',
})
const saving = ref(false)

async function loadTables() {
  try {
    tables.value = (await window.go.main.App.ListTables()) || []
  } catch { tables.value = [] }
}

async function loadColumns(table: string) {
  if (!table || columnsByTable.value[table]) return
  try {
    const info = await window.go.main.App.DescribeTable(table)
    columnsByTable.value = { ...columnsByTable.value, [table]: info?.columns || [] }
  } catch {
    columnsByTable.value = { ...columnsByTable.value, [table]: [] }
  }
}

async function loadRelationships() {
  loading.value = true
  try {
    relationships.value = (await window.go.main.App.ListRelationships()) || []
  } catch { relationships.value = [] }
  loading.value = false
}

function openAddForm() {
  form.value = {
    parent_table: '',
    parent_column: '',
    child_table: '',
    child_column: '',
    on_delete: 'NO ACTION',
  }
  showAddForm.value = true
}

async function onParentTableChange() {
  form.value.parent_column = ''
  await loadColumns(form.value.parent_table)
}

async function onChildTableChange() {
  form.value.child_column = ''
  await loadColumns(form.value.child_table)
}

async function saveRelationship() {
  saving.value = true
  try {
    await window.go.main.App.AddForeignKey({
      parent_table: form.value.parent_table,
      parent_column: form.value.parent_column,
      child_table: form.value.child_table,
      child_column: form.value.child_column,
      on_delete: form.value.on_delete,
      on_update: 'NO ACTION',
    })
    showAddForm.value = false
    await loadRelationships()
  } catch (e: any) {
    errorMessage.value = (e && (e.message || e)) ? String(e.message || e) : 'Failed to add relationship'
  } finally {
    saving.value = false
  }
}

async function removeRelationship(id: string) {
  if (!id) return
  try {
    await window.go.main.App.RemoveForeignKey(id)
    await loadRelationships()
  } catch (e: any) {
    errorMessage.value = (e && (e.message || e)) ? String(e.message || e) : 'Failed to remove relationship'
  }
}

onMounted(async () => {
  await loadTables()
  await loadRelationships()
})
</script>

<template>
  <div class="relationships-view flex flex-col h-full">
    <!-- Header -->
    <div class="page-subhead" style="padding: 14px 24px;">
      <h1 style="font-size: 22px;">Relationships</h1>
      <span class="flex-1"></span>
      <button @click="openAddForm" class="btn btn-primary btn-sm">
        <Icon name="plus" :size="14" /> Add relationship
      </button>
    </div>

    <!-- Relationship diagram / list -->
    <div class="flex-1 overflow-auto p-6">
      <div v-if="loading" class="sh-mono text-[var(--sh-graphite)]">Loading…</div>
      <div v-else-if="relationships.length === 0" class="empty-hero">
        <div class="glyph"><Icon name="database" :size="48" /></div>
        <h2>No relationships yet</h2>
        <p class="lead">Declare a foreign key to relate two tables.</p>
        <button @click="openAddForm" class="btn btn-primary"><Icon name="plus" :size="16" /> Add relationship</button>
      </div>
      <div v-else class="space-y-3">
        <div
          v-for="rel in relationships"
          :key="rel.id"
          class="rel-card card card-pad flex items-center gap-4"
        >
          <!-- parent -> child diagram -->
          <div class="rel-node">
            <span class="rel-table">{{ rel.parent_table }}</span>
            <span class="rel-col sh-mono">{{ rel.parent_column }}</span>
          </div>
          <span class="rel-arrow"><Icon name="arrowRight" :size="18" /></span>
          <div class="rel-node">
            <span class="rel-table">{{ rel.child_table }}</span>
            <span class="rel-col sh-mono">{{ rel.child_column }}</span>
          </div>
          <span class="badge badge--teal">ON DELETE {{ rel.on_delete }}</span>
          <span class="flex-1"></span>
          <button
            @click="removeRelationship(rel.id)"
            class="btn btn-ghost btn-sm rel-remove"
            style="color: var(--sh-error);"
          >
            <Icon name="trash" :size="14" /> Remove
          </button>
        </div>
      </div>
    </div>

    <!-- Add relationship modal -->
    <div v-if="showAddForm" class="modal-overlay" @click.self="showAddForm = false">
      <div class="card card-pad w-full max-w-lg">
        <h3 class="card-title mb-4">Add relationship</h3>
        <div class="space-y-3 mb-4">
          <div>
            <label class="field-label">Parent table</label>
            <select v-model="form.parent_table" @change="onParentTableChange" class="field-input rel-parent-table">
              <option value="" disabled>Select table…</option>
              <option v-for="t in tables" :key="t" :value="t">{{ t }}</option>
            </select>
          </div>
          <div>
            <label class="field-label">Parent column</label>
            <select v-model="form.parent_column" class="field-input rel-parent-column">
              <option value="" disabled>Select column…</option>
              <option v-for="c in (columnsByTable[form.parent_table] || [])" :key="c.name" :value="c.name">{{ c.name }}</option>
            </select>
          </div>
          <div>
            <label class="field-label">Child table</label>
            <select v-model="form.child_table" @change="onChildTableChange" class="field-input rel-child-table">
              <option value="" disabled>Select table…</option>
              <option v-for="t in tables" :key="t" :value="t">{{ t }}</option>
            </select>
          </div>
          <div>
            <label class="field-label">Child column</label>
            <select v-model="form.child_column" class="field-input rel-child-column">
              <option value="" disabled>Select column…</option>
              <option v-for="c in (columnsByTable[form.child_table] || [])" :key="c.name" :value="c.name">{{ c.name }}</option>
            </select>
          </div>
          <div>
            <label class="field-label">On delete</label>
            <select v-model="form.on_delete" class="field-input rel-on-delete">
              <option>NO ACTION</option>
              <option>CASCADE</option>
              <option>RESTRICT</option>
              <option>SET NULL</option>
            </select>
          </div>
        </div>
        <div class="flex gap-3 justify-end">
          <button @click="showAddForm = false" class="btn btn-ghost">Cancel</button>
          <button @click="saveRelationship" :disabled="saving" class="btn btn-primary">Add foreign key</button>
        </div>
      </div>
    </div>

    <div v-if="errorMessage" class="error-toast">
      {{ errorMessage }}
    </div>
  </div>
</template>

<style scoped>
.rel-node {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.rel-table {
  font-weight: 600;
  color: var(--sh-ink);
}
.rel-col {
  font-size: 12px;
  color: var(--sh-graphite);
}
.rel-arrow {
  color: var(--sh-teal);
  display: inline-flex;
  align-items: center;
}
.error-toast {
  position: fixed;
  bottom: 24px;
  right: 24px;
  background: var(--sh-error);
  color: var(--sh-paper);
  padding: 12px 18px;
  border: 2px solid var(--sh-ink);
  font-weight: 600;
  z-index: 100;
}
</style>
