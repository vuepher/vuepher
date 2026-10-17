/**
 * The page object the Go server sends, on the first visit inside the
 * `data-page` attribute of the app element and afterwards as the JSON response
 * body.
 *
 * The shape is a contract: it is versioned by the framework's ProtocolVersion
 * and documented in docs/view-bridge.md. Changing it requires a matching release
 * of this package.
 */
export interface PageObject<TProps = Record<string, unknown>> {
  /** The Vue component to render, e.g. "Posts/Index". */
  component: string

  /** The values handed to the component. */
  props: TProps

  /** The current URL, including the query string. */
  url: string

  /** The asset version. A mismatch makes the server answer 409. */
  version: string

  /** Props that were resolved lazily on this request. */
  deferredProps?: string[]

  /** Props that should be merged with, rather than replace, client state. */
  mergeProps?: string[]

  /** Replace the history entry instead of pushing a new one. */
  clearHistory?: boolean
}

/** A resolved page, as held by the client. */
export interface Page<TProps = Record<string, unknown>> {
  component: string
  props: TProps
  url: string
  version: string
  deferredProps: string[]
}

/**
 * Resolves a component name to a component definition.
 *
 * The usual implementation is `import.meta.glob`, which Vite turns into a map of
 * lazily imported modules:
 *
 *   resolve: (name) => import.meta.glob('./Pages/**\/*.vue')[`./Pages/${name}.vue`]()
 */
export type PageResolver = (name: string) => Promise<unknown> | unknown

/** Vue's component type. */
export type ComponentDefinition = import('vue').Component

/** Progress bar control, passed to a custom implementation. */
export interface ProgressController {
  start(): void
  setPercent(percent: number): void
  finish(): void
  remove(): void
}

/** Options for {@link createVuepherApp}. */
export interface VuepherAppOptions {
  /** Resolves a component name to a definition. Required. */
  resolve: PageResolver

  /**
   * Runs once after the Vue app is created and before the first page renders.
   * Install plugins, register global components and set the error handler here.
   */
  setup?: (context: SetupContext) => void

  /**
   * Replaces the built-in progress bar. Return null to disable it.
   *
   * TODO(phase-2): the default delay and minimum-duration values are fixed; make
   * them configurable once the defaults have been tried in real applications.
   */
  progress?: ProgressController | null

  /**
   * Called when a visit fails with a non-page response, for example a network
   * error or an unexpected 500. Returning false suppresses the default full page
   * reload.
   */
  onError?: (error: VisitFailure) => void | boolean
}

/** Context handed to the setup callback. */
export interface SetupContext {
  /** The Vue application, for installing plugins and registering components. */
  app: import('vue').App

  /** The initial page's props. */
  props: Record<string, unknown>

  /** The reactive page state. */
  page: Page

  /** The router, for a redirect decided at setup time. */
  router: Router
}

/** The public router surface. */
export interface Router {
  visit(url: string, options?: VisitOptions): Promise<void>
  reload(options?: VisitOptions): Promise<void>
  current(): Page
  version(): string
}

/** Options accepted by a visit. */
export interface VisitOptions {
  method?: HttpMethod
  data?: Record<string, unknown>
  headers?: Record<string, string>
  /**
   * Send the payload as FormData instead of JSON. Required for file uploads
   * because only multipart supports progress reporting.
   */
  forceFormData?: boolean
  preserveScroll?: boolean
  preserveState?: boolean
  replace?: boolean
  only?: string[]
  except?: string[]
  errorBag?: string
  onStart?: () => void
  onSuccess?: (page: Page) => void
  onError?: (errors: ErrorBag) => void
  onFinish?: () => void
  onProgress?: (percent: number | null) => void
}

/** HTTP methods a visit can use. */
export type HttpMethod =
  | 'get'
  | 'post'
  | 'put'
  | 'patch'
  | 'delete'
  | 'GET'
  | 'POST'
  | 'PUT'
  | 'PATCH'
  | 'DELETE'

/** Failures that are not page objects. */
export interface VisitFailure {
  url: string
  status: number | null
  response?: Response
  error?: unknown
}

/**
 * Validation errors, keyed by field name.
 *
 * Field names match the `json` tag on the Go request struct, which is what makes
 * `errors.title` line up with the input bound to `form.data.title`.
 */
export interface ErrorBag {
  [field: string]: string
}

/** A paginated response, matching the Go paginator's shape. */
export interface Paginated<T> {
  data: T[]
  meta: {
    current_page: number
    per_page: number
    total: number
    last_page: number
    from: number
    to: number
    has_more: boolean
  }
  links: {
    first: string
    prev: string
    next: string
    last: string
    path: string
  }
}

/** Shared props the frameework injects on every page. */
/** Application metadata shared with every page (VIEW-05). */
export interface AppMeta {
  name?: string
  env?: string
  debug?: boolean
}

export interface SharedProps {
  /** Flash messages set with `ctx.Redirect(...).With("success", "...")`. */
  flash?: Record<string, string>

  /** Validation errors flashed by the previous request. */
  errors?: ErrorBag

  /** Input flashed by the previous request, for repopulating a failed form. */
  old?: Record<string, unknown>

  /** The authenticated user, when a guard is installed. */
  auth?: { user: unknown | null }

  /** The CSRF token, sent with every non-GET visit. */
  csrf_token?: string

  /**
   * Application metadata, so a layout can render the app name or a debug banner
   * without the controller passing it on every page. Sent by the ShareProps
   * middleware; opt out with `SharePropsOptions.DisableApp`.
   */
  app?: AppMeta

  /** The document title, when the controller shares one. */
  title?: string
}
