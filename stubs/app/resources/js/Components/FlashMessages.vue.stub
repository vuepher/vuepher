<script setup lang="ts">
import { computed } from 'vue'
import { usePage } from '@vuepher/vue3'

/**
 * Renders the flash messages a controller set with
 *
 *   return ctx.Redirect("/posts").With("success", "Post created")
 *
 * The middleware injects them as the shared `flash` prop, so a component does
 * not have to be passed anything: flash data travels with the redirect.
 */
const page = usePage()

const messages = computed(() => {
  const flash = (page.props.flash ?? {}) as Record<string, string>
  return Object.entries(flash).map(([type, text]) => ({ type, text }))
})
</script>

<template>
  <div v-if="messages.length" class="flash-stack">
    <p
      v-for="message in messages"
      :key="message.type"
      class="flash"
      :class="`flash-${message.type}`"
      role="status"
    >
      {{ message.text }}
    </p>
  </div>
</template>

<style scoped>
.flash-stack {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  padding: 0.75rem 1.25rem;
}
.flash {
  margin: 0;
  padding: 0.6rem 0.9rem;
  border-radius: 6px;
  border: 1px solid var(--border);
}
.flash-success {
  border-color: var(--success);
  color: var(--success);
}
.flash-error {
  border-color: var(--danger);
  color: var(--danger);
}
</style>
