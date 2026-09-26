<script setup lang="ts">
/**
 * One theme in the Appearance picker: a miniature of the Studio shell drawn
 * from the theme's own colours, plus name / type / source. A theme that
 * failed validation is shown with its error and cannot be chosen.
 */
import { computed } from 'vue'
import Icon from './Icon.vue'
import type { ThemeListing } from '../theme/useTheme'

const props = defineProps<{
  listing: ThemeListing
  selected: boolean
  /** Shown when this theme is currently applied via "follow system". */
  activeVia?: 'system' | null
}>()
const emit = defineEmits<{ select: [id: string] }>()

const c = computed(() => props.listing.resolved?.colors)
const disabled = computed(() => !props.listing.resolved)
</script>

<template>
  <button
    type="button"
    class="theme-card"
    :class="{ 'is-selected': selected, 'is-broken': disabled }"
    :disabled="disabled"
    :aria-pressed="selected"
    :data-theme-id="listing.id"
    :data-testid="`theme-card-${listing.id}`"
    @click="emit('select', listing.id)"
  >
    <span v-if="c" class="theme-card__preview" aria-hidden="true" :style="{ background: c['bg'] }">
      <span class="theme-card__bar" :style="{ background: c['chrome.bg'], borderColor: c['chrome.border'] }">
        <span class="theme-card__mark" :style="{ background: c['accent'], borderColor: c['chrome.fg'] }"></span>
        <span class="theme-card__pill" :style="{ borderColor: c['chrome.fg'] }"></span>
      </span>
      <span class="theme-card__side" :style="{ background: c['sidebar.bg'], borderColor: c['border'] }">
        <span class="theme-card__nav" :style="{ background: c['sidebar.active.bg'], borderColor: c['sidebar.active.fg'] }"></span>
        <span class="theme-card__nav theme-card__nav--muted" :style="{ background: c['fg.muted'] }"></span>
        <span class="theme-card__nav theme-card__nav--muted" :style="{ background: c['fg.muted'] }"></span>
      </span>
      <span class="theme-card__main">
        <span class="theme-card__h" :style="{ background: c['fg'] }"></span>
        <span class="theme-card__card" :style="{ background: c['surface'], borderColor: c['border'], boxShadow: `2px 2px 0 0 ${c['shadow']}` }">
          <span class="theme-card__btn" :style="{ background: c['accent'] }"></span>
          <span class="theme-card__line" :style="{ background: c['fg.muted'] }"></span>
        </span>
        <span class="theme-card__swatches">
          <span :style="{ background: c['highlight'] }"></span>
          <span :style="{ background: c['attention'] }"></span>
          <span :style="{ background: c['success'] }"></span>
          <span :style="{ background: c['info'] }"></span>
          <span :style="{ background: c['code.bg'] }"></span>
        </span>
      </span>
    </span>
    <span v-else class="theme-card__preview theme-card__preview--broken" aria-hidden="true">
      <Icon name="alert" :size="22" />
    </span>

    <span class="theme-card__meta">
      <span class="theme-card__name">
        <Icon :name="listing.type === 'dark' ? 'moon' : 'sun'" :size="13" />
        {{ listing.name }}
        <span v-if="selected" class="theme-card__check"><Icon name="check" :size="14" /></span>
      </span>
      <span class="theme-card__desc">
        <template v-if="listing.error">{{ listing.error }}</template>
        <template v-else>{{ listing.description || (listing.source === 'user' ? listing.path : '') }}</template>
      </span>
      <span class="theme-card__tags">
        <span class="theme-card__tag">{{ listing.type }}</span>
        <span v-if="listing.source === 'user'" class="theme-card__tag">custom</span>
        <span v-if="activeVia === 'system'" class="theme-card__tag theme-card__tag--live">active · system</span>
      </span>
    </span>
  </button>
</template>

<style scoped>
.theme-card {
  display: flex; flex-direction: column; gap: 10px;
  min-width: 0; width: 100%; overflow: hidden;
  padding: 10px;
  text-align: left;
  background: var(--sh-surface);
  color: var(--sh-ink);
  border: 2px solid var(--vibe-border);
  border-radius: var(--sh-radius-sm);
  box-shadow: var(--sh-shadow-sm);
  cursor: pointer;
  transition: transform var(--sh-dur) var(--sh-ease), box-shadow var(--sh-dur) var(--sh-ease);
  font: inherit;
}
.theme-card:hover:not(:disabled) { transform: translate(-1px, -1px); box-shadow: var(--sh-shadow); }
.theme-card.is-selected { border-color: var(--vibe-accent); box-shadow: var(--sh-shadow-teal); }
.theme-card.is-broken { cursor: not-allowed; opacity: 0.75; border-style: dashed; }

.theme-card__preview {
  position: relative; display: block;
  width: 100%; height: 96px; overflow: hidden;
  align-self: stretch;
  border: 1.5px solid var(--vibe-border);
  border-radius: var(--sh-radius-sm);
}
.theme-card__preview span { display: block; }
.theme-card__bar, .theme-card__side, .theme-card__main, .theme-card__card, .theme-card__swatches { display: flex !important; }
.theme-card__preview--broken { display: flex; align-items: center; justify-content: center; color: var(--sh-error); background: var(--sh-paper-2); }
.theme-card__bar { position: absolute; top: 0; left: 0; right: 0; height: 16px; border-bottom: 1.5px solid; display: flex; align-items: center; gap: 4px; padding: 0 6px; }
.theme-card__mark { width: 8px; height: 8px; border: 1px solid; border-radius: 1px; }
.theme-card__pill { margin-left: auto; width: 22px; height: 6px; border: 1px solid; border-radius: 999px; opacity: 0.8; }
.theme-card__side { position: absolute; top: 16px; bottom: 0; left: 0; width: 34%; border-right: 1.5px solid; padding: 6px 4px; display: flex; flex-direction: column; gap: 4px; }
.theme-card__nav { height: 7px; border-left: 2px solid; border-radius: 1px; }
.theme-card__nav--muted { border: 0; opacity: 0.35; height: 5px; width: 70%; }
.theme-card__main { position: absolute; top: 16px; bottom: 0; right: 0; width: 66%; padding: 8px; display: flex; flex-direction: column; gap: 6px; }
.theme-card__h { height: 7px; width: 55%; border-radius: 1px; }
.theme-card__card { flex: 1; border: 1.5px solid; border-radius: 2px; padding: 5px; display: flex; flex-direction: column; gap: 4px; }
.theme-card__btn { width: 34%; height: 8px; border-radius: 1px; }
.theme-card__line { width: 80%; height: 4px; opacity: 0.5; border-radius: 1px; }
.theme-card__swatches { display: flex; gap: 3px; }
.theme-card__swatches span { flex: 1; height: 6px; border-radius: 1px; }

.theme-card__meta { display: flex; flex-direction: column; gap: 3px; min-width: 0; max-width: 100%; }
.theme-card__name { display: flex; align-items: center; gap: 6px; font-weight: 600; font-size: 14px; }
.theme-card__name svg { color: var(--sh-graphite); flex: none; }
.theme-card__check { margin-left: auto; color: var(--vibe-accent); display: inline-flex; }
.theme-card__desc { display: block; max-width: 100%; font-size: 12px; color: var(--sh-graphite); line-height: 1.4; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.is-broken .theme-card__desc { color: var(--sh-error); white-space: normal; }
.theme-card__tags { display: flex; gap: 6px; margin-top: 2px; }
.theme-card__tag {
  font-family: var(--sh-font-mono); font-size: 10px; text-transform: uppercase; letter-spacing: 0.06em;
  color: var(--sh-graphite); border: 1px solid var(--vibe-border-subtle); border-radius: 999px; padding: 1px 6px;
}
.theme-card__tag--live { color: var(--vibe-accent); border-color: var(--vibe-accent); }
</style>
