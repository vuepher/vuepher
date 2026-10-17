import type { Page, Router } from './types'

/**
 * The module-level registry that usePage() and useVuepherForm() read from.
 *
 * Vuepher has no plugin installation step, so composables cannot receive the
 * router through provide/inject without the application having to wrap them. A
 * module-level reference keeps the API to one line:
 *
 *   const form = useVuepherForm({ title: '' })
 *
 * The trade-off is that only one Vuepher application may exist per page, which is
 * true of any application that mounts once at `#app` and gets a clear error when
 * it is not.
 */
let activeRouter: Router | null = null
let activePage: Page | null = null

/** Installs the active application. Called by createVuepherApp. */
export function setActiveContext(router: Router, page: Page): void {
  activeRouter = router
  activePage = page
}

/** Removes the active application, for tests and for unmounting. */
export function clearActiveContext(): void {
  activeRouter = null
  activePage = null
}

/** Returns the active router. Throws when the app was never created. */
export function getRouter(): Router {
  if (activeRouter === null) {
    throw new Error(
      '[vuepher] no active application. Call createVuepherApp() in your entry point ' +
        'before using useVuepherForm() or router.visit().',
    )
  }
  return activeRouter
}

/**
 * Returns the reactive page. The object is the same one the router mutates, so
 * reading `page.props` or `page.url` inside a computed or a template tracks
 * changes without any extra wiring.
 */
export function getPage<TProps = Record<string, unknown>>(): Page<TProps> {
  if (activePage === null) {
    throw new Error(
      '[vuepher] no active page. Call createVuepherApp() in your entry point ' +
        'before using usePage().',
    )
  }
  // The stored page is typed against the default props shape; the caller's TProps
  // is a claim about what the server sent, which only the application can make.
  return activePage as unknown as Page<TProps>
}

/** Reports whether an application has been created. Used by health checks and tests. */
export function hasActiveContext(): boolean {
  return activeRouter !== null
}
