<script setup lang="ts">
// SignInBanner — across the top of the main pane while a connection needs the
// person to sign in with an AWS profile (#131). "Sign in" runs the device flow:
// the page opens, the code shows here to check against it, and the banner goes
// when the connection (and the lakes on it) is ready. "Not now" hides it for
// this open of the workspace only. The banner starts its own tracking on mount,
// as WorkspaceBanner does for the .env prompt.
import { onMounted, nextTick } from 'vue'
import Icon from './Icon.vue'
import SignInCode from './SignInCode.vue'
import { signInItems, startSignIn, cancelSignIn, signInNotNow, startSignInTracking } from '../composables/useSignIn'

onMounted(startSignInTracking)

// The buttons swap places (Sign in and Not now become Cancel and back): keep the
// keyboard focus on the one that took their place.
async function act(e: Event, go: () => Promise<void> | void, next: string) {
  const banner = (e.currentTarget as HTMLElement).closest('[data-testid="signin-banner"]')
  await go()
  await nextTick()
  ;(banner?.querySelector(`[data-testid="${next}"]`) as HTMLElement | null)?.focus()
}
</script>

<template>
  <div v-for="it in signInItems" :key="it.profile" class="ws-banner" role="status" data-testid="signin-banner">
    <Icon name="key" :size="20" class="lead-ico" />
    <div class="bt">
      <template v-if="it.prompt">
        <b>Finish signing in to AWS profile {{ it.profile }}.</b>
        Approve the sign-in in the page that opened, after checking that it shows this code:
        <SignInCode :prompt="it.prompt" />
      </template>
      <template v-else>
        <b v-if="it.reason === 'expired'">AWS session for profile {{ it.profile }} has expired.</b>
        <b v-else>You aren't signed in to AWS profile {{ it.profile }}.</b>
        Sign in to use <template v-for="(n, i) in it.names" :key="n"><code>{{ n }}</code><template v-if="i < it.names.length - 1">, </template></template> and what reads through it.
        <span class="sub">Or run <code>aws sso login --profile {{ it.profile }}</code> in a terminal.</span>
      </template>
      <span v-if="it.error" class="sub err-text" role="alert" data-testid="signin-error">{{ it.error }}</span>
    </div>
    <div class="row">
      <button v-if="it.prompt" class="btn btn-sm" type="button" data-testid="signin-cancel" @click="act($event, () => cancelSignIn(it.profile), 'signin-start')">Cancel</button>
      <template v-else>
        <button class="btn btn-primary btn-sm" type="button" :disabled="it.starting" data-testid="signin-start" @click="act($event, () => startSignIn(it.connection), 'signin-cancel')">{{ it.starting ? 'Starting…' : 'Sign in' }}</button>
        <button class="btn btn-sm" type="button" :disabled="it.starting" data-testid="signin-notnow" @click="signInNotNow(it.profile)">Not now</button>
      </template>
    </div>
  </div>
</template>
