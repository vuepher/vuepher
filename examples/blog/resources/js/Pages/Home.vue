<script setup lang="ts">
import AppLayout from '@/Layouts/AppLayout.vue'
import { VuepherLink } from '@vuepher/vue3'

/**
 * The landing page.
 *
 * Every prop here came from HomeController.Index() in Go:
 *
 *   return ctx.View("Home", vuepher.Props{"greeting": ..., "version": ...})
 */
defineProps<{
  greeting: string
  version: string
}>()
</script>

<template>
  <AppLayout title="Home">
    <h1>{{ greeting }}</h1>
    <p class="lead">
      This page was rendered by a Go controller and handed to Vue 3 through the
      view bridge. There is no REST endpoint in between.
    </p>

    <ul class="checklist">
      <li>No separate API to build or keep in sync</li>
      <li>Routing, validation and data access all run in Go</li>
      <li>One binary to deploy</li>
    </ul>

    <p>
      <VuepherLink href="/posts" class="button">See the example CRUD</VuepherLink>
    </p>

    <p class="muted">Framework version {{ version }}</p>
  </AppLayout>
</template>

<style scoped>
h1 {
  margin-top: 0;
}
.lead {
  max-width: 60ch;
}
.checklist {
  padding-left: 1.2rem;
  line-height: 1.9;
}
.muted {
  color: var(--muted);
  font-size: 0.85rem;
}
</style>
