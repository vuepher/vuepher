<script setup lang="ts">
import { computed } from 'vue'
import { VuepherLink, usePage } from '@vuepher/vue3'
import FlashMessages from '@/Components/FlashMessages.vue'

withDefaults(defineProps<{ title?: string }>(), { title: '' })

const page = usePage()

// usePage() is reactive: the user and flash messages update on every visit
// without the layout re-mounting, which is what makes navigation feel instant.
//
// `auth` is shared by the guard bootstrap/app.go installs, so it is always
// present; `user` is null until an application adds a login flow. v0.1 ships the
// guard and the middleware, not the login/register screens.
const user = computed(() => (page.props.auth as { user?: unknown } | undefined)?.user)
const userName = computed(() => (user.value as { name?: string } | null | undefined)?.name ?? null)

const documentTitle = computed(() => {
  const name = (page.props.app as { name?: string } | undefined)?.name ?? 'Vuepher'
  return page.props.title ? `${page.props.title} - ${name}` : name
})
</script>

<template>
  <div class="layout">
    <header class="layout-header">
      <VuepherLink href="/" class="brand">{{ documentTitle }}</VuepherLink>

      <nav class="layout-nav">
        <VuepherLink href="/posts">Posts</VuepherLink>
        <span v-if="userName" class="muted">{{ userName }}</span>
      </nav>
    </header>

    <FlashMessages />

    <main class="layout-main">
      <slot />
    </main>

    <footer class="layout-footer">
      <p>Built with Vuepher. Progress bar and layouts persist between visits.</p>
    </footer>
  </div>
</template>

<style scoped>
.layout {
  display: flex;
  min-height: 100vh;
  flex-direction: column;
}
.layout-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  padding: 0.75rem 1.25rem;
  border-bottom: 1px solid var(--border);
}
.brand {
  font-weight: 600;
  text-decoration: none;
}
.layout-nav {
  display: flex;
  gap: 1rem;
}
.layout-main {
  flex: 1;
  padding: 1.5rem 1.25rem;
}
.layout-footer {
  padding: 1rem 1.25rem;
  border-top: 1px solid var(--border);
  color: var(--muted);
  font-size: 0.85rem;
}
</style>
