/**
 * @vuepher/vue3 — the Vue 3 client for the Vuepher framework.
 *
 * The Go side renders page components from controllers; this package is the
 * client that makes navigation between them feel like an SPA without a REST API
 * in between. See docs/view-bridge.md for the protocol.
 *
 * Typical usage, in resources/js/app.ts:
 *
 *   import { createVuepherApp } from '@vuepher/vue3'
 *
 *   createVuepherApp({
 *     resolve: (name) => {
 *       const pages = import.meta.glob('./Pages/**\/*.vue')
 *       const page = pages[`./Pages/${name}.vue`]
 *       if (!page) throw new Error(`Page "${name}" not found. Create it with: vuepher make:view ${name}`)
 *       return page()
 *     },
 *   }).mount('#app')
 */

// Bootstrap (FE-01)
export { createVuepherApp, readEmbeddedPageObject, type VuepherApp } from './app'

// Navigation (FE-02, FE-05)
export { VuepherLink } from './link'
export { createRouter, Headers, type PageState, type SwapOptions, type VuepherRouter } from './router'

// Composables (FE-03, FE-04)
export { useVuepherForm, type SubmitOptions, type VuepherForm } from './useForm'
export {
  useActivePath,
  useCurrentUrl,
  useErrors,
  usePage,
  usePageProps,
} from './usePage'

// Progress bar (FE-06)
export { createProgressBar } from './progress'

// Escape hatches for tests and advanced setups.
export {
  clearActiveContext,
  getPage,
  getRouter,
  hasActiveContext,
  setActiveContext,
} from './context'

// Types
export type {
  ComponentDefinition,
  ErrorBag,
  HttpMethod,
  Page,
  PageObject,
  PageResolver,
  Paginated,
  ProgressController,
  Router,
  SetupContext,
  SharedProps,
  VisitFailure,
  VisitOptions,
  VuepherAppOptions,
} from './types'
