import { createVuepherApp } from '@vuepher/vue3'
import '../css/app.css'

/**
 * The application entry point.
 *
 * Pages are resolved by `import.meta.glob` at build time, which means Vite knows
 * about every page component and can code-split them without a route table to
 * keep in sync. Naming convention: `Posts/Index.vue` is the component
 * "Posts/Index", which is the string a Go controller passes to ctx.View().
 */
const app = createVuepherApp({
  resolve: (name) => {
    const pages = import.meta.glob('./Pages/**/*.vue', { eager: false })
    const load = pages[`./Pages/${name}.vue`]

    if (!load) {
      throw new Error(
        `Vuepher: page component "${name}" was not found. ` +
          `Expected ./Pages/${name}.vue. ` +
          `Create it with: vuepher make:view ${name}`,
      )
    }

    // The glob returns a loader, not the component: call it and return the
    // promise. Passing the loader itself makes Vue treat it as a function
    // component and render "[object Promise]" instead of the page.
    return load()
  },

  // Set up runs once, after Vue is created but before the first page renders.
  // Install plugins, a global error handler and shared global components here.
  setup: ({ app, props }) => {
    app.config.globalProperties.$page = props

    app.config.errorHandler = (error, _instance, info) => {
      // In development this keeps errors visible in the console. Wire it to a
      // reporting service for production.
      console.error('[vuepher] unhandled error', { error, info })
    }
  },
})

app.mount('#app')
