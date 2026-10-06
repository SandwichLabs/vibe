<script setup lang="ts">
// ConnectionDialogs — mounts the connection prompts once, inside the shell, so
// the rail, the selector, the editor and a failed query can all raise them.
import CredentialsPromptDialog from './CredentialsPromptDialog.vue'
import ExtensionConsentDialog from './ExtensionConsentDialog.vue'
</script>

<template>
  <CredentialsPromptDialog />
  <ExtensionConsentDialog />
</template>
