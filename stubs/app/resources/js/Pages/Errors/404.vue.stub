<script setup lang="ts">
import { VuepherLink } from '@vuepher/vue3'

/**
 * The 404 page.
 *
 * Error pages are ordinary Vue components (VIEW-09): the framework resolves the
 * component from the status code. bootstrap/app.go maps 403, 404 and 500 to
 * these files.
 *
 * `message` is set by the framework from the error returned by the handler, and
 * `debug` is only present outside production.
 */
defineProps<{
  status: number
  message: string
  debug?: string
}>()
</script>

<template>
  <div class="error-page">
    <h1>Page not found</h1>
    <p>{{ message || 'The page you were looking for does not exist.' }}</p>
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
