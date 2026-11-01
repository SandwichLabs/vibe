<script setup lang="ts">
// GlossaryPanel — semantics/glossary.md rendered with the app's one safe
// markdown renderer (the file may come from a clone or an agent's accepted
// proposal, so it is never raw HTML), with the raw file a click away.
import { computed } from 'vue'
import { renderSafeMarkdown } from '../lib/markdown'
import SourceText from './SourceText.vue'

const props = defineProps<{ detail: DefinitionDetail }>()
const html = computed(() => renderSafeMarkdown(props.detail.glossary?.text ?? props.detail.text))
</script>

<template>
  <div class="col" style="gap: var(--sh-space-4);" data-testid="glossary-panel">
    <h2 class="card-title" style="margin: 0; font-size: 28px;">Glossary</h2>
    <div v-if="detail.error" class="note attn" role="alert" data-testid="glossary-error"><span>The glossary cannot be read: {{ detail.error }}</span></div>
    <p v-else-if="!detail.glossary?.terms.length && !detail.text" class="help" style="margin: 0;" data-testid="glossary-empty">
      There is no glossary yet. Write <code>semantics/glossary.md</code> with a <code>## Term</code> heading per term, or have the assistant propose one.
    </p>
    <div v-else class="prose assay-content assay-md-content" data-testid="glossary-rendered" v-html="html"></div>
    <SourceText v-if="detail.text" :text="detail.text" path="semantics/glossary.md" :open="!!detail.error" />
  </div>
</template>
