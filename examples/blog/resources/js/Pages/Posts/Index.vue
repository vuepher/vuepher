<script setup lang="ts">
import { computed } from 'vue'
import { VuepherLink, useVuepherForm } from '@vuepher/vue3'
import AppLayout from '@/Layouts/AppLayout.vue'
import Pagination from '@/Components/Pagination.vue'

interface Post {
  id: number
  title: string
  body: string
  published: boolean
}

interface Paginated<T> {
  data: T[]
  meta: { current_page: number; per_page: number; total: number; last_page: number; from: number; to: number; has_more: boolean }
  links: { first: string; prev: string; next: string; last: string; path: string }
}

/**
 * Props come from PostController.Index():
 *
 *   posts, err := models.Post{}.Query().Latest().Paginate(ctx, 15)
 *   return ctx.View("Posts/Index", vuepher.Props{"posts": posts})
 */
const props = defineProps<{ posts: Paginated<Post> }>()

const posts = computed(() => props.posts.data)

/**
 * useVuepherForm owns the whole submit lifecycle:
 *
 *   data       the reactive payload, bind inputs to it with v-model
 *   errors     validation messages flashed by the Go controller
 *   processing true while the request is in flight
 *   post/put/delete  send the request and follow the redirect automatically
 *
 * Validation stays in Go. The form simply renders whatever came back, so the
 * rules live in one place and cannot drift from the server.
 */
const form = useVuepherForm({ title: '', body: '' })
</script>

<template>
  <AppLayout title="Posts">
    <h1>Posts</h1>

    <section class="card">
      <h2>New post</h2>

      <form @submit.prevent="form.post('/posts')">
        <label for="title">Title</label>
        <input id="title" v-model="form.data.title" type="text" autocomplete="off">
        <!-- The error is keyed by the json tag on the Go request struct. -->
        <p v-if="form.errors.title" class="field-error">{{ form.errors.title }}</p>

        <label for="body">Body</label>
        <textarea id="body" v-model="form.data.body" rows="4" />
        <p v-if="form.errors.body" class="field-error">{{ form.errors.body }}</p>

        <button type="submit" class="button" :disabled="form.processing">
          {{ form.processing ? 'Saving…' : 'Save' }}
        </button>
        <button type="button" class="button button-secondary" @click="form.reset()">
          Reset
        </button>
      </form>
    </section>

    <section class="card">
      <h2>All posts</h2>

      <p v-if="posts.length === 0" class="muted">
        Nothing here yet. Create the first post above, or run
        <code>vuepher db:seed</code>.
      </p>

      <ul v-else class="post-list">
        <li v-for="post in posts" :key="post.id">
          <VuepherLink :href="`/posts/${post.id}`">{{ post.title }}</VuepherLink>
          <span v-if="!post.published" class="badge">draft</span>
        </li>
      </ul>

      <Pagination :paginator="props.posts" />
    </section>
  </AppLayout>
</template>

<style scoped>
.card {
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 1rem 1.25rem;
  margin-bottom: 1.25rem;
}
label {
  display: block;
  margin: 0.75rem 0 0.25rem;
  font-size: 0.9rem;
}
input,
textarea {
  width: 100%;
  padding: 0.5rem 0.6rem;
  border: 1px solid var(--border);
  border-radius: 6px;
  font: inherit;
  box-sizing: border-box;
}
.field-error {
  margin: 0.25rem 0 0;
  color: var(--danger);
  font-size: 0.85rem;
}
.post-list {
  padding-left: 1.1rem;
  line-height: 2;
}
.badge {
  margin-left: 0.5rem;
  padding: 0.1rem 0.4rem;
  border: 1px solid var(--border);
  border-radius: 999px;
  font-size: 0.7rem;
  color: var(--muted);
  text-transform: uppercase;
}
.muted {
  color: var(--muted);
}
</style>
