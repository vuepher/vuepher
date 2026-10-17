import { createApp, defineComponent, h, reactive, shallowRef, type App, type Component } from 'vue'

import { setActiveContext } from './context'
import { createProgressBar } from './progress'
import { createRouter, type PageState, type VuepherRouter } from './router'
import type { ProgressController, VuepherAppOptions } from './types'

/** What {@link createVuepherApp} returns. */
export interface VuepherApp {
  /** The Vue application, for installing plugins after creation. */
  app: App

  /** Mounts the application. */
  mount(selector: string | Element): VuepherApp

  /** The router, for programmatic visits. */
  router: VuepherRouter

  /** The reactive page state. */
  state: PageState

  /** The progress bar, or null when disabled. */
  progress: ProgressController | null

  /** Unmounts the application and clears the active context. */
  unmount(): void
}

/**
 * createVuepherApp() boots the client (FE-01).
 *
 *   const app = createVuepherApp({
 *     resolve: (name) => {
 *       const pages = import.meta.glob('./Pages/**\/*.vue')
 *       const page = pages[`./Pages/${name}.vue`]
 *       if (!page) throw new Error(`Page "${name}" not found. Create it with: vuepher make:view ${name}`)
 *       return page()
 *     },
 *   })
 *
 *   app.mount('#app')
 *
 * The resolver is the only required option, because the one thing this package
 * cannot know is where a project keeps its page components.
 */
export function createVuepherApp(options: VuepherAppOptions): VuepherApp {
  if (typeof options.resolve !== 'function') {
    throw new Error(
      '[vuepher] createVuepherApp requires a resolve function. It maps a component name ' +
        'such as "Posts/Index" to a component definition; import.meta.glob is the usual implementation.',
    )
  }

  const state = reactive<PageState>({
    component: '',
    props: {},
    url: '',
    version: '',
    deferredProps: [],
    resolved: null,
  })

  // The component is held in a shallowRef rather than inside the reactive page
  // object: a Vue component is not meant to be made reactive, and doing so makes
  // Vue walk the whole component definition.
  const component = shallowRef<Component | null>(null)

  // A remount counter. Incrementing it forces Vue to build a fresh instance, which
  // is what makes onMounted run again after a navigation that should not preserve
  // state.
  const remountKey = shallowRef(0)
  let renderedComponent = ''

  const progress =
    options.progress === undefined ? createProgressBar() : options.progress

  const router = createRouter({
    state,
    resolve: options.resolve,
    progress,
    onError: options.onError,
    onPage: (resolved, page, swap) => {
      const reuse = swap.preserveState === true && renderedComponent === page.component
      component.value = resolved as Component
      if (!reuse) {
        remountKey.value += 1
      }
      renderedComponent = page.component
    },
  })

  // The root component renders whichever page is current. It is a separate
  // component rather than a render function on a shared instance so that Vue's
  // devtools show "VuepherRoot" above the page, which makes the layout of a
  // rendered application legible.
  const Root = defineComponent({
    name: 'VuepherRoot',
    setup() {
      return () => {
        const page = component.value
        if (page === null) {
          return null
        }
        // `key` is a special prop in h(): passing it here is what controls
        // whether Vue reuses the previous instance.
        return h(page, { key: remountKey.value, ...state.props })
      }
    },
  })

  const app = createApp(Root)

  setActiveContext(router, state)

  options.setup?.({ app, props: state.props, page: state, router })

  // The initial page is rendered after setup so that anything setup installs is
  // available to the first component.
  void router.initialise().then(() => {
    router.listen()
  })

  return {
    app,
    progress,
    router,
    state,
    mount(selector: string | Element) {
      app.mount(selector)
      return this
    },
    unmount() {
      app.unmount()
      progress?.remove()
    },
  }
}

/**
 * Reads the page object the server embedded in the initial HTML without mounting
 * anything.
 *
 * Useful for a build-time check or a test that wants to assert what the server
 * sent.
 */
export function readEmbeddedPageObject(): unknown {
  const root = document.getElementById('app')
  const raw = root?.dataset.page
  if (!raw) {
    return null
  }
  try {
    return JSON.parse(raw)
  } catch {
    return null
  }
}
