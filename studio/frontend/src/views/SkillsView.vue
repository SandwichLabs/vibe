<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import Icon from '../components/Icon.vue'
import { useModalKeys } from '../composables/useModal'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import ConflictBanner from '../components/ConflictBanner.vue'
import { renderSafeMarkdown } from '../lib/markdown'
import { EventsOn } from '../wailsjs/runtime/runtime'
import { register as registerPendingSave } from '../composables/usePendingSaves'
import { errorText, isConflict } from '../lib/errors'

// SkillsView (#62): lists built-in + file (user/import) Skills, shows a
// rendered/raw detail pane, and lets the user author or edit file skills.
// Built-ins are read-only (they're embedded in the binary, not files in
// skills/) — the UI never offers edit/delete for them.
const skills = ref<SkillSummary[]>([])
const loading = ref(true)
const selectedName = ref<string | null>(null)
const errorMessage = ref('')
const viewMode = ref<'rendered' | 'raw'>('rendered')

const showEditor = ref(false)
const editorEl = ref<HTMLElement | null>(null)
useModalKeys(showEditor, () => { showEditor.value = false }, editorEl)

watch(errorMessage, (msg) => {
  if (msg) setTimeout(() => { if (errorMessage.value === msg) errorMessage.value = '' }, 4000)
})
const editingOriginalName = ref<string | null>(null) // null = creating new
const editingRev = ref("") // the revision the skill had when the editor opened
const formName = ref('')
const formDescription = ref('')
const formInstructions = ref('')
const formTableName = ref('')
const formNameError = ref('')
const saving = ref(false)

// The skill's file changed on disk while the editor had it open (a git pull,
// another editor, the agent): ConflictBanner offers reload / keep mine / copy.
const conflict = ref<{ deleted: boolean } | null>(null)
const conflictBusy = ref(false)
// The revision on disk when the banner was raised: "Keep mine" only overwrites that one.
let conflictRev = ''
function raiseConflict(deleted: boolean) {
  conflict.value = { deleted }
  conflictRev = ''
  const id = editingOriginalName.value ?? formName.value
  if (deleted || !id) return
  window.go.main.App.ListSkills().then((all: SkillSummary[]) => {
    if (conflict.value && (editingOriginalName.value ?? formName.value) === id) conflictRev = all.find((x) => x.name === id)?.rev ?? ''
  }).catch(() => { /* falls back to an unconditional keep */ })
}
// What the editor was opened with, to tell "I edited" from "untouched".
let openedWith = ''
const formSnapshot = () => JSON.stringify([formDescription.value, formInstructions.value, formTableName.value])

const showConfirmDelete = ref(false)
const pendingDeleteName = ref<string | null>(null)

const NAME_PATTERN = /^[a-z0-9][a-z0-9-]{0,63}$/

const selected = computed(() => skills.value.find((s) => s.name === selectedName.value) || null)

function sourceLabel(source: string) {
  return source === 'import' ? 'Imported' : source
}

function sourceBadgeClass(source: string) {
  if (source === 'built-in') return 'badge--teal'
  if (source === 'import') return 'badge--graphite'
  return 'badge--warn'
}

async function loadSkills() {
  loading.value = true
  try {
    skills.value = (await window.go.main.App.ListSkills()) || []
    if (skills.value.length > 0 && !selectedName.value) {
      selectedName.value = skills.value[0].name
    }
  } catch (e: any) {
    errorMessage.value = 'Failed to load skills: ' + errorText(e)
  }
  loading.value = false
}

function selectSkill(name: string) {
  selectedName.value = name
  viewMode.value = 'rendered'
}

function openCreate() {
  editingOriginalName.value = null
  editingRev.value = ''
  formName.value = ''
  formDescription.value = ''
  formInstructions.value = ''
  formTableName.value = ''
  formNameError.value = ''
  conflict.value = null
  showEditor.value = true
  openedWith = formSnapshot()
}

function openEdit(s: SkillSummary) {
  editingOriginalName.value = s.name
  editingRev.value = s.rev || ''
  formName.value = s.name
  formDescription.value = s.description
  formInstructions.value = s.instructions
  formTableName.value = s.table_name || ''
  formNameError.value = ''
  conflict.value = null
  showEditor.value = true
  openedWith = formSnapshot()
}

async function submitEditor() {
  if (!NAME_PATTERN.test(formName.value)) {
    formNameError.value = 'Lowercase letters, numbers, and hyphens only (e.g. "revenue-rules")'
    return
  }
  if (!formDescription.value.trim() || !formInstructions.value.trim()) {
    formNameError.value = 'Description and instructions are required'
    return
  }
  saving.value = true
  try {
    await window.go.main.App.SaveSkill(formName.value, formDescription.value, formInstructions.value, formTableName.value, editingRev.value)
    showEditor.value = false
    await loadSkills()
    selectedName.value = formName.value
    viewMode.value = 'rendered'
  } catch (e: any) {
    if (isConflict(e)) {
      raiseConflict(false)
      formNameError.value = ''
    } else {
      formNameError.value = errorText(e)
    }
  }
  saving.value = false
}

// flushPending is registered with usePendingSaves so switching workspaces,
// closing and quitting save an open, edited skill first. A conflict or an
// unfinished skill makes it fail so the caller can ask.
async function flushPending() {
  if (!showEditor.value || formSnapshot() === openedWith) return
  if (conflict.value) throw new Error('this skill changed on disk and your edit is not saved yet')
  if (!NAME_PATTERN.test(formName.value) || !formDescription.value.trim() || !formInstructions.value.trim()) {
    throw new Error('the skill being edited is not finished (it needs a name, description and instructions)')
  }
  try {
    await window.go.main.App.SaveSkill(formName.value, formDescription.value, formInstructions.value, formTableName.value, editingRev.value)
  } catch (e: any) {
    if (isConflict(e)) raiseConflict(false)
    throw new Error(errorText(e))
  }
  await loadSkills()
  editingRev.value = skills.value.find((x) => x.name === formName.value)?.rev || ''
  openedWith = formSnapshot()
}
let unregisterPending: (() => void) | undefined

// resolveConflict applies the person's choice (see ConflictBanner). "reload"
// refills the form from the disk version; "overwrite" writes the form over it;
// "saveCopy" writes the form as <name>-copy and shows the disk version here.
async function resolveConflict(action: 'reload' | 'overwrite' | 'saveCopy') {
  const id = editingOriginalName.value ?? formName.value
  if (action === 'reload' && conflict.value?.deleted) {
    // The file is gone: "Close it" just closes the editor.
    conflict.value = null
    showEditor.value = false
    await loadSkills()
    return
  }
  conflictBusy.value = true
  try {
    const res = await window.go.main.App.ResolveConflict({
      kind: 'skill', id, action, rev: conflictRev,
      data: { description: formDescription.value, instructions: formInstructions.value, tableName: formTableName.value },
    })
    conflict.value = null
    conflictRev = ''
    if (action === 'overwrite') {
      editingRev.value = res.rev
      showEditor.value = false
      await loadSkills()
      selectedName.value = id
      return
    }
    await loadSkills()
    const disk = skills.value.find((x) => x.name === id)
    if (disk) {
      editingRev.value = disk.rev || ''
      formDescription.value = disk.description
      formInstructions.value = disk.instructions
      formTableName.value = disk.table_name || ''
      openedWith = formSnapshot()
    } else {
      showEditor.value = false
    }
    if (action === 'saveCopy') errorMessage.value = `Saved your version as "${res.title}".`
  } catch (e: any) {
    if (isConflict(e)) {
      // The file changed again after the banner was raised: show it afresh.
      raiseConflict(false)
      formNameError.value = 'The file changed on disk again, so nothing was overwritten. Choose again.'
    } else {
      formNameError.value = errorText(e)
    }
  } finally {
    conflictBusy.value = false
  }
}

// onDiskChange: another program changed skills/. The open editor refills quietly
// when the person hasn't touched it, and shows the banner when they have.
function onDiskChange(p: { kind: string; id: string; op: string }) {
  if (p?.kind !== 'skill' || !showEditor.value || p.id !== editingOriginalName.value) return
  if (formSnapshot() === openedWith && p.op !== 'deleted') {
    void loadSkills().then(() => {
      const disk = skills.value.find((x) => x.name === p.id)
      if (!disk) return
      editingRev.value = disk.rev || ''
      formDescription.value = disk.description
      formInstructions.value = disk.instructions
      formTableName.value = disk.table_name || ''
      openedWith = formSnapshot()
    })
    return
  }
  raiseConflict(p.op === 'deleted')
}

function deleteSkill(s: SkillSummary) {
  pendingDeleteName.value = s.name
  showConfirmDelete.value = true
}

async function confirmDelete() {
  const name = pendingDeleteName.value
  showConfirmDelete.value = false
  pendingDeleteName.value = null
  if (!name) return
  try {
    await window.go.main.App.DeleteSkill(name)
    if (selectedName.value === name) selectedName.value = null
    await loadSkills()
  } catch (e: any) {
    errorMessage.value = 'Failed to delete skill: ' + errorText(e)
  }
}

// Import/app-import writes a workspace-<table> skill; other views (Import,
// the agent) mutating data should refresh this list without a manual reload.
let unsubDataChanged: (() => void) | undefined
let unsubDisk: (() => void) | undefined
onMounted(() => {
  unregisterPending = registerPendingSave(() => 'The skill editor', flushPending)
  loadSkills()
  unsubDataChanged = EventsOn('data:changed', loadSkills)
  unsubDisk = EventsOn('workspace:changed-on-disk', onDiskChange)
})
onUnmounted(() => {
  unregisterPending?.()
  unsubDataChanged?.()
  unsubDisk?.()
})
</script>

<template>
  <div class="row" style="height: 100%; align-items: stretch; gap: 0;">
    <!-- Skill list -->
    <div class="col" style="width: 300px; border-right: var(--sh-border); background: var(--sh-paper-2); gap: 0;">
      <div class="row" style="justify-content: space-between; padding: var(--sh-space-3) var(--sh-space-4); border-bottom: var(--sh-border);">
        <h2 class="sh-label" style="margin: 0;">Skills</h2>
        <button @click="openCreate" class="btn btn-primary btn-sm" data-testid="skill-new"><Icon name="plus" :size="14" /> New</button>
      </div>
      <div style="flex: 1; overflow: auto;">
        <div v-if="loading" class="sh-mono" style="padding: var(--sh-space-4); color: var(--sh-graphite);">Loading…</div>
        <div v-else-if="skills.length === 0" class="sh-mono" style="padding: var(--sh-space-4); color: var(--sh-graphite); text-align: center;">No skills yet</div>
        <div
          v-for="s in skills"
          :key="s.name"
          @click="selectSkill(s.name)"
          class="row"
          data-testid="skill-row"
          :data-skill-name="s.name"
          :class="{ 'row-selected': selectedName === s.name }"
          style="justify-content: space-between; padding: var(--sh-space-2) var(--sh-space-4); cursor: pointer; border-bottom: var(--sh-border-thin);"
        >
          <div style="min-width: 0;">
            <div class="row" style="gap: var(--sh-space-1); align-items: center; min-width: 0;">
              <Icon name="bookOpen" :size="13" style="color: var(--sh-graphite); flex: none;" />
              <div style="font-size: var(--sh-fs-sm); font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">{{ s.name }}</div>
            </div>
            <div class="row" style="gap: var(--sh-space-1); margin-top: var(--sh-space-1);">
              <span class="badge" :class="sourceBadgeClass(s.source)">{{ sourceLabel(s.source) }}</span>
              <span v-if="s.table_name" class="badge badge--graphite" style="white-space: nowrap;" :title="`Loads only while ${s.table_name} has rows`">table: {{ s.table_name }}</span>
            </div>
          </div>
          <div class="row" style="gap: var(--sh-space-1);">
            <button v-if="s.source !== 'built-in'" @click.stop="openEdit(s)" class="btn btn-ghost btn-sm" title="Edit skill"><Icon name="edit" :size="14" /></button>
            <button v-if="s.source !== 'built-in'" @click.stop="deleteSkill(s)" class="btn btn-ghost btn-sm" style="color: var(--sh-error);" title="Delete skill"><Icon name="trash" :size="14" /></button>
          </div>
        </div>
      </div>
    </div>

    <!-- Detail pane -->
    <div style="flex: 1; overflow: auto; padding: var(--sh-space-6);">
      <div v-if="!selected" class="sh-mono" style="color: var(--sh-graphite); text-align: center; margin-top: var(--sh-space-10);">Select a skill to view</div>
      <div v-else>
        <div class="row" style="justify-content: space-between; align-items: flex-start; margin-bottom: var(--sh-space-4);">
          <div>
            <div class="row" style="gap: var(--sh-space-2); align-items: center;">
              <h2 class="card-title" style="margin: 0; font-size: 28px;">{{ selected.name }}</h2>
              <span class="badge" :class="sourceBadgeClass(selected.source)">{{ sourceLabel(selected.source) }}</span>
            </div>
            <p class="sh-mono" style="color: var(--sh-graphite); margin-top: var(--sh-space-1);">{{ selected.description }}</p>
          </div>
          <div class="seg" role="group" aria-label="Skill view">
            <button type="button" :class="{ on: viewMode === 'rendered' }" :aria-pressed="viewMode === 'rendered'" @click="viewMode = 'rendered'" data-testid="skill-view-rendered">Rendered</button>
            <button type="button" :class="{ on: viewMode === 'raw' }" :aria-pressed="viewMode === 'raw'" @click="viewMode = 'raw'" data-testid="skill-view-raw">Raw</button>
          </div>
        </div>

        <div v-if="viewMode === 'rendered'" class="prose vibe-content vibe-md-content" data-testid="skill-rendered" v-html="renderSafeMarkdown(selected.instructions)"></div>
        <pre v-else class="sh-mono" data-testid="skill-raw" style="white-space: pre-wrap; padding: var(--sh-space-4); background: var(--sh-paper-2); border: var(--sh-border); border-radius: var(--sh-radius);">{{ selected.instructions }}</pre>
      </div>
    </div>

    <!-- Editor modal -->
    <div v-if="showEditor" class="modal-overlay" @click.self="showEditor = false" role="dialog" aria-modal="true" :aria-label="editingOriginalName ? 'Edit skill' : 'New skill'">
      <div ref="editorEl" class="card card-pad" style="width: 100%; max-width: 560px;">
        <h3 class="card-title" style="margin: 0 0 var(--sh-space-4);">{{ editingOriginalName ? 'Edit skill' : 'New skill' }}</h3>
        <ConflictBanner
          v-if="conflict"
          kind="skill"
          :path="`skills/${editingOriginalName ?? formName}/SKILL.md`"
          :deleted="conflict.deleted"
          :busy="conflictBusy"
          style="margin-bottom: var(--sh-space-3);"
          @reload="resolveConflict('reload')"
          @overwrite="resolveConflict('overwrite')"
          @save-copy="resolveConflict('saveCopy')"
        />
        <div class="col" style="gap: 4px;">
          <label class="sh-label" style="margin: 0;">Name</label>
          <input v-model="formName" :disabled="!!editingOriginalName" placeholder="e.g. revenue-rules" class="field-input" style="width: 100%;" data-testid="skill-name-input" />
        </div>
        <div class="col" style="gap: 4px; margin-top: var(--sh-space-3);">
          <label class="sh-label" style="margin: 0;">Description</label>
          <input v-model="formDescription" placeholder="One line — advertised to the agent up front" class="field-input" style="width: 100%;" data-testid="skill-description-input" />
        </div>
        <div class="col" style="gap: 4px; margin-top: var(--sh-space-3);">
          <label class="sh-label" style="margin: 0;">Instructions (markdown)</label>
          <textarea v-model="formInstructions" rows="10" class="field-input" style="width: 100%; min-height: 220px;" data-testid="skill-instructions-input" placeholder="Full instructions the agent reads via read_skill…"></textarea>
        </div>
        <div class="col" style="gap: 4px; margin-top: var(--sh-space-3);">
          <label class="sh-label" style="margin: 0;">Bound table (optional)</label>
          <input v-model="formTableName" placeholder="Only load while this table has rows" class="field-input" style="width: 100%;" data-testid="skill-table-input" />
        </div>
        <p v-if="formNameError" style="font-size: var(--sh-fs-xs); margin-top: var(--sh-space-2); font-weight: 600; color: var(--sh-error);">{{ formNameError }}</p>
        <div class="row" style="gap: var(--sh-space-3); justify-content: flex-end; margin-top: var(--sh-space-4);">
          <button @click="showEditor = false" class="btn btn-ghost">Cancel</button>
          <button @click="submitEditor" :disabled="saving || !!conflict" class="btn btn-primary" data-testid="skill-save">{{ saving ? 'Saving…' : 'Save' }}</button>
        </div>
      </div>
    </div>

    <ConfirmDialog
      :visible="showConfirmDelete"
      title="Delete skill"
      confirm-label="Delete"
      message="Delete this skill? This cannot be undone."
      @confirm="confirmDelete"
      @cancel="showConfirmDelete = false; pendingDeleteName = null"
    />

    <div v-if="errorMessage" class="error-toast">{{ errorMessage }}</div>
  </div>
</template>
