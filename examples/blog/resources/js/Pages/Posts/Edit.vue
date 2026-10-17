<script setup lang="ts">
import AppLayout from '@/Layouts/AppLayout.vue'

// Props arrive from the Go controller through the view bridge. Their types are
// generated from the Go structs by `vuepher types:generate`, which runs
// automatically during `vuepher dev`, and land in resources/js/types/generated.
//
// Declare them here once the controller sends something, to get autocomplete and
// type checking inside the template:
//
//   import type { PageProps } from '@/types/generated/pages'
//   defineProps<PageProps['Posts/Edit']>()
//
// There is deliberately no declaration while the page has no props: an empty one
// such as defineProps<Record<string, never>>() reads as "this page takes nothing",
// but Vue's compiler cannot turn Record<string, never> into runtime props and
// fails the build with "Failed to resolve @vue/compiler-sfc".
</script>

<template>
  <AppLayout title="Edit">
    <h1>Edit</h1>
    <p>
      Edit <code>resources/js/Pages/Posts/Edit.vue</code> to get started.
    </p>
  </AppLayout>
</template>
