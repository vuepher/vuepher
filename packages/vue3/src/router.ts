import type {
  HttpMethod,
  Page,
  PageObject,
  PageResolver,
  ProgressController,
  Router,
  VisitFailure,
  VisitOptions,
} from './types'

/** Protocol headers, mirroring the constants in the Go framework. */
export const Headers = {
  request: 'X-Vuepher',
  version: 'X-Vuepher-Version',
  location: 'X-Vuepher-Location',
  partialData: 'X-Vuepher-Partial-Data',
  partialComponent: 'X-Vuepher-Partial-Component',
  partialOnly: 'X-Vuepher-Partial-Only',
  partialExcept: 'X-Vuepher-Partial-Except',
  errorBag: 'X-Vuepher-Error-Bag',
  csrf: 'X-CSRF-Token',
} as const

/** Reactive page holder the router writes into. */
export interface PageState extends Page {
  /** The resolved component definition for the current page. */
  resolved: unknown
}

export interface RouterDependencies {
  /** Reactive state the router keeps in sync. */
  state: PageState

  /** Resolves component names to definitions. */
  resolve: PageResolver

  /** Progress bar, or null to disable. */
  progress: ProgressController | null

  /**
   * Called after a page has been swapped in. The host passes the resolved
   * component so it can decide whether to re-mount.
   */
  onPage: (component: unknown, page: Page, options: SwapOptions) => void

  /** Optional failure hook. */
  onError?: (failure: VisitFailure) => void | boolean
}

/** How a page swap should behave. */
export interface SwapOptions {
  preserveScroll?: boolean
  preserveState?: boolean
  replace?: boolean
}

/** The router plus the pieces the host application needs. */
export interface VuepherRouter extends Router {
  /** Renders the page the server embedded in the initial HTML. */
  initialise(): Promise<void>

  /** Installs the history listener. Called once by the host. */
  listen(): void

  /** The reactive state, for tests and debugging. */
  state: PageState
}

/**
 * createRouter builds the client-side router.
 *
 * It is a plain object rather than a class so that consumers can wrap or replace
 * individual methods without subclassing, which is the usual shape for a small
 * library like this.
 */
/**
 * unwrapComponent normalises what a resolver returns into a component definition.
 *
 * `import.meta.glob` produces loaders whose promise resolves to a module object
 * (`{ default: Component }`), and a resolver may reasonably return the module
 * itself. Handing a module object to h() renders nothing and fails, so the module
 * default is unwrapped here rather than in every application's resolver.
 */
function unwrapComponent(value: unknown): unknown {
  if (value !== null && typeof value === 'object' && 'default' in (value as Record<string, unknown>)) {
    return (value as { default: unknown }).default
  }
  return value
}

export function createRouter(dependencies: RouterDependencies): VuepherRouter {
  const { state, resolve, progress, onPage } = dependencies
  let listening = false
  let inFlight: AbortController | null = null

  /** Reads the page object the server embedded in the initial HTML. */
  function readInitialPage(): PageObject | null {
    const root = document.getElementById('app')
    if (!root) {
      return null
    }
    const raw = root.dataset.page
    if (!raw) {
      return null
    }
    try {
      return JSON.parse(raw) as PageObject
    } catch (error) {
      console.error('[vuepher] the embedded page object could not be parsed', error)
      return null
    }
  }

  /**
   * Applies a page object to the client state.
   *
   * `preserveState` keeps the currently mounted component instance when the
   * component name has not changed, which is what makes a filter or a search
   * input survive a visit. `preserveScroll` keeps the scroll position instead of
   * jumping to the top.
   */
  async function swapPage(object: PageObject, options: SwapOptions = {}): Promise<void> {
    const previousComponent = state.component
    const previousScroll = { x: window.scrollX, y: window.scrollY }

    let component: unknown
    try {
      component = unwrapComponent(await resolve(object.component))
    } catch (error) {
      console.error(
        `[vuepher] could not load the page component "${object.component}". ` +
          `Check that resources/js/Pages/${object.component}.vue exists.`,
        error,
      )
      // Without a component there is nothing to render; a full load at least
      // shows the server's own error page.
      window.location.href = object.url
      return
    }

    state.component = object.component
    state.props = object.props
    state.url = object.url
    state.version = object.version
    state.deferredProps = object.deferredProps ?? []
    state.resolved = component

    // The component is set before onPage so the host can decide whether the
    // instance may be reused.
    onPage(component, state, options)

    // History. A redirect after a write must not leave the intermediate URL in
    // the stack, otherwise Back returns to a page that only existed for a moment.
    const replace = options.replace ?? object.clearHistory ?? false
    const historyState = { vuepher: true, component: object.component }
    try {
      if (replace) {
        window.history.replaceState(historyState, '', object.url)
      } else {
        window.history.pushState(historyState, '', object.url)
      }
    } catch {
      // Cross-origin URLs cannot be pushed; fall through to a real navigation.
      window.location.href = object.url
      return
    }

    applyTitle(object.props)

    if (!options.preserveScroll) {
      window.scrollTo(0, 0)
    } else {
      window.scrollTo(previousScroll.x, previousScroll.y)
    }

    if (previousComponent !== object.component) {
      // A new component should start at the top of its own content.
      window.scrollTo(0, 0)
    }
  }

  /** Updates the document title from the shared props, when the controller sends one. */
  function applyTitle(props: Record<string, unknown>): void {
    const title = props.title
    if (typeof title === 'string' && title !== '') {
      document.title = title
    }
  }

  /** Builds the request headers for a visit. */
  function buildHeaders(options: VisitOptions, partial: boolean, currentComponent: string): Record<string, string> {
    const headers: Record<string, string> = {
      [Headers.request]: 'true',
      [Headers.version]: state.version,
      Accept: 'text/html, application/xhtml+xml',
      'X-Requested-With': 'XMLHttpRequest',
    }

    if (partial) {
      // The component name is what the server compares against, so that a
      // partial reload addressed elsewhere is served in full. The only/except
      // lists travel in their own headers rather than being folded together with
      // it.
      headers[Headers.partialData] = 'true'
      headers[Headers.partialComponent] = currentComponent
      if (options.only && options.only.length > 0) {
        headers[Headers.partialOnly] = options.only.join(',')
      }
      if (options.except && options.except.length > 0) {
        headers[Headers.partialExcept] = options.except.join(',')
      }
    }

    if (options.errorBag) {
      headers[Headers.errorBag] = options.errorBag
    }

    // The CSRF token travels in the header rather than in the payload, so a
    // form post and a JSON post are protected the same way.
    const token = state.props.csrf_token
    if (typeof token === 'string' && token !== '') {
      headers[Headers.csrf] = token
    }

    return { ...headers, ...(options.headers ?? {}) }
  }

  /** Serialises the payload for the request. */
  function buildBody(
    options: VisitOptions,
  ): { body: BodyInit | undefined; contentType: string | null } {
    if (options.method === undefined || options.method.toLowerCase() === 'get') {
      return { body: undefined, contentType: null }
    }
    const data = options.data ?? {}

    const hasFile = options.forceFormData === true || containsFile(data)
    if (hasFile) {
      // Only multipart supports upload progress, so a file forces the slow path.
      const form = new FormData()
      for (const [key, value] of Object.entries(data)) {
        appendToFormData(form, key, value)
      }
      return { body: form, contentType: null }
    }

    return { body: JSON.stringify(data), contentType: 'application/json' }
  }

  /** Performs a visit. */
  async function visit(url: string, options: VisitOptions = {}): Promise<void> {
    const method = (options.method ?? 'get').toUpperCase() as Uppercase<HttpMethod>
    const partial = method === 'GET' && ((options.only?.length ?? 0) > 0 || (options.except?.length ?? 0) > 0)

    options.onStart?.()
    progress?.start()

    // Cancel a visit that is still in flight: the newest navigation is what the
    // user asked for, and letting both complete would race on the page state.
    if (inFlight) {
      inFlight.abort()
    }
    inFlight = new AbortController()
    const signal = inFlight.signal

    const { body, contentType } = buildBody({ ...options, method })
    const headers = buildHeaders(options, partial, state.component)
    if (contentType) {
      headers['Content-Type'] = contentType
    }

    let response: Response
    try {
      response = await fetch(url, { method, headers, body, signal, credentials: 'same-origin' })
    } catch (error) {
      // An aborted request is a superseded visit, not a failure.
      if (signal.aborted) {
        return
      }
      progress?.finish()
      options.onFinish?.()
      handleFailure({ url, status: null, error })
      return
    }

    // A version mismatch means the client is running an older build than the
    // server: reloading is the only correct response (VIEW-04).
    if (response.status === 409) {
      progress?.finish()
      options.onFinish?.()
      const location = response.headers.get(Headers.location)
      window.location.href = location ?? url
      return
    }

    const contentTypeHeader = response.headers.get('Content-Type') ?? ''
    const isPageObject = contentTypeHeader.includes('application/json')

    if (!isPageObject) {
      // The server answered with a full document. Usually a redirect that left
      // the SPA, or an error page: hand the navigation to the browser.
      progress?.finish()
      options.onFinish?.()
      window.location.href = response.url || url
      return
    }

    let pageObject: PageObject
    try {
      pageObject = (await response.json()) as PageObject
    } catch (error) {
      progress?.finish()
      options.onFinish?.()
      handleFailure({ url, status: response.status, response, error })
      return
    }

    await swapPage(pageObject, {
      preserveScroll: options.preserveScroll,
      preserveState: options.preserveState ?? isSameComponent(pageObject, options),
      // A redirect after a write should replace the entry: the POST URL is not
      // somewhere Back should return to.
      replace: options.replace ?? response.redirected,
    })

    progress?.finish()

    const errors = (pageObject.props.errors ?? {}) as Record<string, string>
    if (Object.keys(errors).length > 0) {
      options.onError?.(errors)
    } else {
      options.onSuccess?.(state)
    }
    options.onFinish?.()
  }

  /** Reports a failure to the configured handler, falling back to a full load. */
  function handleFailure(failure: VisitFailure): void {
    console.error(
      `[vuepher] the visit to ${failure.url} did not return a page object` +
        (failure.status ? ` (status ${failure.status})` : ''),
      failure.error ?? '',
    )
    const handled = dependencies.onError?.(failure)
    if (handled !== false) {
      window.location.href = failure.url
    }
  }

  /** Reloads the current page, optionally requesting only some props. */
  async function reload(options: VisitOptions = {}): Promise<void> {
    const only = options.only ?? (state.deferredProps.length > 0 ? state.deferredProps : undefined)
    await visit(state.url, {
      ...options,
      method: 'get',
      preserveScroll: true,
      preserveState: true,
      only,
    })
  }

  /** Initialises the client from the server-rendered page object. */
  async function initialise(): Promise<void> {
    const initial = readInitialPage()
    if (!initial) {
      console.error(
        '[vuepher] no page object was found. The app root element must be ' +
          'rendered by the framework, with the page object in its data-page attribute.',
      )
      return
    }

    state.component = initial.component
    state.props = initial.props
    state.url = initial.url || window.location.pathname + window.location.search
    state.version = initial.version
    state.deferredProps = initial.deferredProps ?? []

    let component: unknown
    try {
      component = unwrapComponent(await resolve(initial.component))
    } catch (error) {
      console.error(`[vuepher] could not load the page component "${initial.component}"`, error)
      return
    }
    state.resolved = component

    // The initial render always pushes, so Back has somewhere to go.
    window.history.replaceState({ vuepher: true, component: initial.component }, '', state.url)
    applyTitle(initial.props)
    onPage(component, state, { preserveScroll: true, preserveState: false })
  }

  /** Installs the history listener. */
  function listen(): void {
    if (listening) {
      return
    }
    listening = true

    window.addEventListener('popstate', () => {
      void visit(window.location.href, { preserveScroll: false, replace: true })
    })
  }

  return {
    initialise,
    listen,
    visit,
    reload,
    current: () => state,
    version: () => state.version,
    state,
  }
}

/** Reports whether a page object targets the component already displayed. */
function isSameComponent(pageObject: PageObject, options: VisitOptions): boolean {
  if (options.preserveState !== undefined) {
    return options.preserveState
  }
  // Only a GET to the same component can safely reuse the mounted instance;
  // anything else deserves a fresh component so that onMounted runs again.
  const method = (options.method ?? 'get').toLowerCase()
  return method === 'get' && pageObject.component !== undefined
}

/** Reports whether a payload contains a File, which forces multipart encoding. */
export function containsFile(data: Record<string, unknown>): boolean {
  for (const value of Object.values(data)) {
    if (value instanceof File || value instanceof Blob) {
      return true
    }
    if (Array.isArray(value) && value.some((item) => item instanceof File || item instanceof Blob)) {
      return true
    }
  }
  return false
}

/** Appends a value to FormData, handling arrays and nested objects. */
export function appendToFormData(form: FormData, key: string, value: unknown): void {
  if (value === null || value === undefined) {
    form.append(key, '')
    return
  }
  if (value instanceof File || value instanceof Blob) {
    form.append(key, value)
    return
  }
  if (Array.isArray(value)) {
    value.forEach((item, index) => appendToFormData(form, `${key}[${index}]`, item))
    return
  }
  if (typeof value === 'object') {
    for (const [nestedKey, nestedValue] of Object.entries(value as Record<string, unknown>)) {
      appendToFormData(form, `${key}[${nestedKey}]`, nestedValue)
    }
    return
  }
  form.append(key, String(value))
}
