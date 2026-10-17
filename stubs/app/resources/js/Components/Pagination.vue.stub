<script setup lang="ts">
import { computed } from 'vue'
import { VuepherLink } from '@vuepher/vue3'

/**
 * Renders an orm.Paginator.
 *
 * The Go side is shaped so this component needs no adapter: `data`, `meta` and
 * `links` map straight onto what is rendered here (DB-08).
 */
interface PageMeta {
  current_page: number
  per_page: number
  total: number
  last_page: number
  from: number
  to: number
  has_more: boolean
}

interface PageLinks {
  first: string
  prev: string
  next: string
  last: string
  path: string
}

interface Paginated<T> {
  data: T[]
  meta: PageMeta
  links: PageLinks
}

const props = defineProps<{ paginator: Paginated<unknown> }>()

const meta = computed(() => props.paginator.meta)
const links = computed(() => props.paginator.links)

const summary = computed(() => {
  if (meta.value.total === 0) {
    return 'No results'
  }
  return `Showing ${meta.value.from}\u2013${meta.value.to} of ${meta.value.total}`
})
</script>

<template>
  <nav v-if="meta.last_page > 1 || meta.total > 0" class="pagination" aria-label="Pagination">
    <span class="pagination-summary">{{ summary }}</span>

    <div class="pagination-controls">
      <!-- VuepherLink intercepts the click and performs an SPA visit, so only
           the data changes: the page does not reload and scroll is preserved. -->
      <VuepherLink
        v-if="links.prev"
        :href="links.prev"
        class="pagination-button"
        preserve-scroll
      >
        Previous
      </VuepherLink>
      <span v-else class="pagination-button is-disabled">Previous</span>

      <span class="pagination-page">Page {{ meta.current_page }} of {{ meta.last_page }}</span>

      <VuepherLink
        v-if="links.next"
        :href="links.next"
        class="pagination-button"
        preserve-scroll
      >
        Next
      </VuepherLink>
      <span v-else class="pagination-button is-disabled">Next</span>
    </div>
  </nav>
</template>

<style scoped>
.pagination {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  flex-wrap: wrap;
  padding-top: 1rem;
}
.pagination-controls {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}
.pagination-button {
  padding: 0.35rem 0.7rem;
  border: 1px solid var(--border);
  border-radius: 6px;
  text-decoration: none;
}
.pagination-button.is-disabled {
  opacity: 0.45;
}
.pagination-summary,
.pagination-page {
  color: var(--muted);
  font-size: 0.85rem;
}
</style>
