<script setup lang="ts">
import { VuepherLink } from '@vuepher/vue3'

/**
 * The 500 page.
 *
 * In development `debug` carries the error detail, because a stack trace is the
 * fastest route to a fix. In production the framework omits it and the message
 * is the generic one; the real error goes to the application log.
 */
defineProps<{
  status: number
  message: string
  debug?: string
}>()
</script>

<template>
  <div class="error-page">
    <h1>Something went wrong</h1>
    <p>{{ message || 'The server encountered an error handling this request.' }}</p>
    <VuepherLink href="/" class="button">Back to home</VuepherLink>

    <pre v-if="debug" class="debug">{{ debug }}</pre>
  </div>
</template>

<style scoped>
.error-page {
  max-width: 48rem;
  margin: 4rem auto;
  padding: 0 1.25rem;
  text-align: center;
}
.debug {
  margin-top: 2rem;
  padding: 1rem;
  border: 1px solid var(--border);
  border-radius: 6px;
  overflow-x: auto;
  text-align: left;
  font-size: 0.8rem;
  color: var(--muted);
}
</style>
