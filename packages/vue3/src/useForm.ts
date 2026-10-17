import { computed, reactive, ref, shallowRef, type ComputedRef, type Ref } from 'vue'

import { getRouter } from './context'
import type { ErrorBag, HttpMethod } from './types'

/** Per-submit overrides. */
export interface SubmitOptions {
  preserveScroll?: boolean
  preserveState?: boolean
  replace?: boolean
  onSuccess?: (page: unknown) => void
  onError?: (errors: ErrorBag) => void
  onFinish?: () => void
}

/** The object returned by {@link useVuepherForm}. */
export interface VuepherForm<TData extends Record<string, any>> {
  /** The reactive payload. Bind inputs to it with v-model. */
  data: TData

  /** Validation errors from the server, keyed by field name. */
  errors: ErrorBag

  /**
   * True while a request is in flight. Bind it to :disabled.
   *
   * The return value is reactive, so this reads as a plain boolean in a template
   * and in a script: `:disabled="form.processing"`, not `form.processing.value`.
   * Returning a `Ref` here would be a trap, because Vue only unwraps refs at the
   * top level of a setup binding — `form.processing` would be a ref *object*,
   * which is always truthy, so the button would stay disabled forever.
   */
  processing: boolean

  /**
   * Upload progress, 0 to 100, or null when it is not measurable.
   *
   * TODO(phase-2): fetch() cannot report upload progress, so this stays null even
   * for a file upload. Byte-level progress needs XMLHttpRequest, which is the
   * next step for FE-06.
   */
  progress: number | null

  /** True after a submit that produced no validation errors. */
  recentlySuccessful: boolean

  /** True when every field currently holds its initial value. */
  isDirty: boolean

  /** Sends the form with POST. */
  post(url: string, options?: SubmitOptions): Promise<void>

  /** Sends the form with PUT. */
  put(url: string, options?: SubmitOptions): Promise<void>

  /** Sends the form with PATCH. */
  patch(url: string, options?: SubmitOptions): Promise<void>

  /** Sends the form with DELETE. */
  delete(url: string, options?: SubmitOptions): Promise<void>

  /** Sends the form with an explicit method. */
  submit(method: HttpMethod, url: string, options?: SubmitOptions): Promise<void>

  /** Restores the initial values. Pass field names to reset only those. */
  reset(...fields: (keyof TData)[]): void

  /** Clears errors. Pass field names to clear only those. */
  clearErrors(...fields: string[]): void

  /** Sets an error for a field, for client-side rules. */
  setError(field: string, message: string): void

  /** Applies a transformation to the payload just before it is sent. */
  transform(fn: (data: TData) => Record<string, unknown>): VuepherForm<TData>

  /** The initial values, as a plain copy. */
  defaults(): TData

  /** Reports whether any error is present. */
  hasErrors: boolean

  /** Sends the payload as multipart, for file uploads. */
  forceFormData(force?: boolean): VuepherForm<TData>
}

/**
 * useVuepherForm() owns the whole submit lifecycle (FE-03):
 *
 *   const form = useVuepherForm({ title: '', body: '' })
 *
 *   <form @submit.prevent="form.post('/posts')">
 *     <input v-model="form.data.title">
 *     <span v-if="form.errors.title">{{ form.errors.title }}</span>
 *     <button :disabled="form.processing">Save</button>
 *   </form>
 *
 * The important design decision is that validation lives on the server. The form
 * does not know the rules; it renders whatever the Go controller sent back. That
 * keeps the rules in one place, where they can also protect a non-browser client,
 * and it is why the error keys are the `json` tag names on the request struct.
 *
 * Errors arrive as a flash message on the page following a failed submit, so the
 * composable reads them from the new page's props rather than from the response
 * body — which is also why a redirect after a failure shows the errors correctly.
 */
export function useVuepherForm<TData extends Record<string, any>>(
  initial: TData,
): VuepherForm<TData> {
  const defaults = clone(initial)
  const data = reactive(clone(initial)) as TData
  const errors = reactive<ErrorBag>({})
  const processing = ref(false)
  const progress = ref<number | null>(null)
  const recentlySuccessful = ref(false)

  const router = getRouter()

  // A transform is a function, and a plain ref holding a function would be
  // invoked as a getter by Vue's unwrapping. shallowRef keeps it as a value.
  const transformer = shallowRef<((data: TData) => Record<string, unknown>) | null>(null)
  const sendAsFormData = ref(false)

  const isDirty = computed(() => !isEqual(data, defaults))
  const hasErrors = computed(() => Object.keys(errors).length > 0)

  /** Replaces the error bag with what the server sent. */
  function applyErrors(next: ErrorBag): void {
    for (const key of Object.keys(errors)) {
      delete errors[key]
    }
    for (const [field, message] of Object.entries(next)) {
      // The framework reports payload-level failures under `_request`, which has
      // no matching input; surfacing it on the form itself is more useful than
      // dropping it.
      errors[field === '_request' ? 'form' : field] = message
    }
  }

  async function send(method: HttpMethod, url: string, options: SubmitOptions = {}): Promise<void> {
    if (processing.value) {
      // A second submit while the first is in flight would double-post. The
      // button is normally disabled, but Enter in a field can still fire it.
      return
    }

    processing.value = true
    recentlySuccessful.value = false
    progress.value = null
    for (const key of Object.keys(errors)) {
      delete errors[key]
    }

    const payload = transformer.value ? transformer.value(data) : { ...data }

    try {
      await router.visit(url, {
        method,
        data: payload,
        forceFormData: sendAsFormData.value,
        preserveScroll: options.preserveScroll ?? true,
        preserveState: options.preserveState ?? false,
        replace: options.replace ?? false,
        onError: (serverErrors) => {
          applyErrors(serverErrors)
          options.onError?.(serverErrors)
        },
        onSuccess: (page) => {
          recentlySuccessful.value = true
          options.onSuccess?.(page)
        },
        onFinish: () => {
          options.onFinish?.()
        },
      })
    } finally {
      processing.value = false
      progress.value = null
    }
  }

  // The object is handed back through reactive(): reading a property then
  // unwraps the ref behind it, so a template sees `processing` as a boolean while
  // the implementation keeps refs to mutate. This is what makes the documented
  // usage (`:disabled="form.processing"`) work.
  const internals: formInternals<TData> = {
    data,
    errors,
    processing,
    progress,
    recentlySuccessful,
    isDirty,
    hasErrors,

    post: (url, options) => send('post', url, options),
    put: (url, options) => send('put', url, options),
    patch: (url, options) => send('patch', url, options),
    delete: (url, options) => send('delete', url, options),
    submit: (method, url, options) => send(method, url, options),

    reset(...fields) {
      if (fields.length === 0) {
        Object.assign(data, clone(defaults))
        return
      }
      for (const field of fields) {
        // A cast is unavoidable: TypeScript cannot prove that assigning
        // defaults[field] to data[field] is sound for a generic key, although it
        // is, because both objects were built from the same initial value.
        ;(data as Record<string, unknown>)[field as string] = (defaults as Record<string, unknown>)[
          field as string
        ]
      }
    },

    clearErrors(...fields) {
      if (fields.length === 0) {
        for (const key of Object.keys(errors)) {
          delete errors[key]
        }
        return
      }
      for (const field of fields) {
        delete errors[field]
      }
    },

    setError(field, message) {
      errors[field] = message
    },

    transform(fn) {
      transformer.value = fn
      return form
    },

    defaults: () => clone(defaults),

    forceFormData(force = true) {
      sendAsFormData.value = force
      return form
    },
  }

  const form = reactive(internals) as unknown as VuepherForm<TData>
  return form
}

// formInternals is the object useVuepherForm builds before it is wrapped in
// reactive(). It declares the reactive state as the refs the implementation
// mutates, while VuepherForm declares how they read once unwrapped.
type formInternals<TData extends Record<string, any>> = Omit<
  VuepherForm<TData>,
  'processing' | 'progress' | 'recentlySuccessful' | 'isDirty' | 'hasErrors'
> & {
  processing: Ref<boolean>
  progress: Ref<number | null>
  recentlySuccessful: Ref<boolean>
  isDirty: ComputedRef<boolean>
  hasErrors: ComputedRef<boolean>
}

/** Deep-copies the payload so the defaults and the live data do not share references. */
function clone<T>(value: T): T {
  if (value === null || typeof value !== 'object') {
    return value
  }
  if (Array.isArray(value)) {
    return value.map((item) => clone(item)) as unknown as T
  }
  if (value instanceof Date) {
    return new Date(value.getTime()) as unknown as T
  }
  if (typeof File !== 'undefined' && value instanceof File) {
    return value
  }
  const out: Record<string, unknown> = {}
  for (const [key, item] of Object.entries(value as Record<string, unknown>)) {
    out[key] = clone(item)
  }
  return out as T
}

/**
 * Reports whether two payloads are equal, used for `isDirty`.
 *
 * Compared field by field rather than by JSON stringify, which is order-dependent
 * and would report a change when a key is added in a different order.
 */
function isEqual(a: unknown, b: unknown): boolean {
  if (a === b) {
    return true
  }
  if (a === null || b === null || typeof a !== 'object' || typeof b !== 'object') {
    return false
  }
  if (Array.isArray(a) !== Array.isArray(b)) {
    return false
  }
  if (Array.isArray(a) && Array.isArray(b)) {
    if (a.length !== b.length) {
      return false
    }
    return a.every((item, index) => isEqual(item, b[index]))
  }
  const left = a as Record<string, unknown>
  const right = b as Record<string, unknown>
  const leftKeys = Object.keys(left)
  const rightKeys = Object.keys(right)
  if (leftKeys.length !== rightKeys.length) {
    return false
  }
  return leftKeys.every((key) => isEqual(left[key], right[key]))
}
