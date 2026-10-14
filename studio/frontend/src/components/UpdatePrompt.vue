<script setup lang="ts">
// Two small, non-blocking notes in the corner: the one-time "Check GitHub for new releases?" question
// (release builds only) and, after a weekly check found one, "Vibe vX is available". Never over a dialog.
import { useRouter } from 'vue-router'
import Icon from './Icon.vue'
import { confirmVisible } from '../composables/useConfirm'
import { showConsent, showNotice, updateNotice, answerUpdateConsent, putOffConsent, skipUpdate, dismissNotice } from '../composables/useUpdates'

const router = useRouter()
const seeUpdate = () => { dismissNotice(); void router.push({ path: '/settings', query: { tab: 'about' } }) }
</script>

<template>
  <div v-if="!confirmVisible && (showConsent || showNotice)" class="update-prompt" role="status" data-testid="update-prompt">
    <template v-if="showConsent">
      <Icon name="refresh" :size="20" class="lead" />
      <div class="bt" data-testid="update-consent">
        <b>Check GitHub for new Vibe releases?</b>
        <span class="sub">Vibe sends nothing but a plain request to github.com. You can change this in Settings &gt; About.</span>
      </div>
      <div class="row">
        <button type="button" class="btn btn-primary btn-sm" data-testid="update-weekly" @click="answerUpdateConsent('weekly')">Check weekly</button>
        <button type="button" class="btn btn-sm" data-testid="update-no" @click="answerUpdateConsent('off')">No thanks</button>
        <button type="button" class="btn btn-sm" data-testid="update-ask-later" @click="putOffConsent">Ask later</button>
      </div>
    </template>
    <template v-else-if="updateNotice">
      <Icon name="download" :size="20" class="lead" />
      <div class="bt" data-testid="update-notice">
        <b>Vibe {{ updateNotice.latest }} is available.</b>
        <span class="sub">You have {{ updateNotice.current }}. Nothing is installed until you confirm.</span>
      </div>
      <div class="row">
        <button type="button" class="btn btn-primary btn-sm" data-testid="update-see" @click="seeUpdate">See what's new</button>
        <button type="button" class="btn btn-sm" data-testid="update-skip" @click="skipUpdate(updateNotice.latest)">Skip this version</button>
        <button type="button" class="btn btn-sm" data-testid="update-dismiss" @click="dismissNotice">Later</button>
      </div>
    </template>
  </div>
</template>

<style>
.update-prompt { position: fixed; left: 16px; bottom: 16px; z-index: 55; width: min(520px, calc(100vw - 32px)); display: flex; flex-wrap: wrap; align-items: center; gap: 12px; padding: 12px 14px; background: var(--sh-surface); color: var(--sh-ink); border: 2px solid var(--vibe-border); border-radius: var(--sh-radius-sm); box-shadow: var(--sh-shadow-lg); }
.update-prompt .lead { flex: none; color: var(--vibe-accent); }
.update-prompt .bt { flex: 1 1 14rem; min-width: 0; font-size: 14px; line-height: 1.45; }
.update-prompt .bt .sub { display: block; font-size: 13px; color: var(--sh-graphite); }
.update-prompt .row { display: flex; flex-wrap: wrap; gap: 8px; }
</style>
