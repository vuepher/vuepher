import { computed, type ComputedRef } from 'vue'

import { getPage } from './context'
import type { ErrorBag, Page, SharedProps } from './types'

/**
 * usePage() returns the current page (FE-04).
 *
 * The returned object is reactive, so a layout can read the authenticated user or
 * a flash message and have it update on the next visit without re-mounting:
 *
 *   const page = usePage<SharedProps>()
 *   const user = computed(() => page.props.auth?.user ?? null)
 *
 * The generic is the props type. Use the generated `PageProps['Posts/Index']`
 * from `vuepher types:generate` to type a page's own props, or SharedProps for
 * everything the framework injects on every page.
 */
export function usePage<TProps extends object = SharedProps>(): Page<TProps> {
  return getPage<TProps>()
}

/**
 * usePageProps() returns only the props, which is what most components want:
 *
 *   const props = usePageProps<{ user: User }>()
 */
export function usePageProps<TProps extends object = SharedProps>(): TProps {
  return getPage<TProps>().props
}

/**
 * useCurrentUrl() returns the current URL. Use it for an active-navigation
 * highlight; compare the path rather than the whole URL so query strings do not
 * break the match.
 */
export function useCurrentUrl(): ComputedRef<string> {
  const page = getPage()
  return computed(() => page.url)
}

/**
 * useActivePath() returns the path of the current page without its query string.
 */
export function useActivePath(): ComputedRef<string> {
  const page = getPage()
  return computed(() => {
    const url = page.url
    const index = url.indexOf('?')
    return index === -1 ? url : url.slice(0, index)
  })
}

/**
 * useErrors() returns the validation errors flashed by the previous request
 * (VIEW-08). An empty object is returned rather than null, so a template can read
 * `errors.title` without a guard.
 */
export function useErrors(): ComputedRef<ErrorBag> {
  const page = getPage<SharedProps>()
  return computed(() => page.props.errors ?? {})
}
