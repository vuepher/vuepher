import { defineComponent, h, type PropType } from 'vue'

import { getRouter } from './context'
import type { HttpMethod } from './types'

/**
 * <VuepherLink> performs an SPA visit instead of a full page load (FE-02).
 *
 *   <VuepherLink href="/posts">Posts</VuepherLink>
 *   <VuepherLink href="/posts" method="post" :data="{ title }">Create</VuepherLink>
 *
 * It renders a real anchor with a real href. That matters more than it looks:
 * middle-click, "open in new tab", right-click → copy link, and browser status
 * preview all keep working, and a JavaScript failure degrades to a normal
 * navigation rather than to nothing.
 *
 * Modifier keys and non-primary buttons are left to the browser, so the component
 * never hijacks an action the user did not intend.
 */
export const VuepherLink = defineComponent({
  name: 'VuepherLink',

  props: {
    /** The destination URL. Required, and rendered as the anchor's href. */
    href: { type: String, required: true },

    /** The HTTP method for the visit. Anything other than GET sends a payload. */
    method: { type: String as PropType<HttpMethod>, default: 'get' },

    /** The payload for a non-GET visit. */
    data: { type: Object as PropType<Record<string, unknown>>, default: () => ({}) },

    /** Keep the scroll position instead of returning to the top. */
    preserveScroll: { type: Boolean, default: false },

    /** Reuse the mounted component when the destination renders the same one. */
    preserveState: { type: Boolean, default: false },

    /** Replace the history entry rather than pushing a new one. */
    replace: { type: Boolean, default: false },

    /** Reload only these props (VIEW-06). */
    only: { type: Array as PropType<string[]>, default: undefined },

    /** Reload everything except these props (VIEW-06). */
    except: { type: Array as PropType<string[]>, default: undefined },

    /**
     * The element to render. Defaults to an anchor; use "button" for an action
     * that happens not to be a navigation but still wants the visit lifecycle.
     */
    as: { type: String, default: 'a' },
  },

  setup(props, { slots, attrs }) {
    function onClick(event: MouseEvent): void {
      // Let the browser handle anything that is not a plain left click: a new
      // tab, a download, a middle click. Intercepting these would be a bug.
      if (event.defaultPrevented) {
        return
      }
      if (event.button !== 0) {
        return
      }
      if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) {
        return
      }

      const target = props.as === 'a' ? (attrs.target as string | undefined) : undefined
      if (target && target !== '_self') {
        return
      }

      event.preventDefault()

      void getRouter().visit(props.href, {
        method: props.method,
        data: props.data,
        preserveScroll: props.preserveScroll,
        preserveState: props.preserveState,
        replace: props.replace,
        only: props.only,
        except: props.except,
      })
    }

    return () => {
      const listeners: Record<string, unknown> = { onClick }

      // A button must not submit an enclosing form by accident.
      if (props.as === 'button') {
        listeners.type = 'button'
      } else {
        listeners.href = props.href
      }

      return h(props.as, { ...attrs, ...listeners }, slots.default?.())
    }
  },
})

export default VuepherLink
