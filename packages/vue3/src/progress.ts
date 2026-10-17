import type { ProgressController } from './types'

/**
 * The built-in navigation progress bar (FE-06).
 *
 * Deliberately tiny and dependency-free: it creates one element, animates its
 * width and removes it. It is not a loading state — the page it belongs to is
 * still interactive during a visit — so the styling aims to be noticeable without
 * being alarming.
 *
 * Behaviour worth knowing:
 *
 *   - start() is delayed slightly, so a visit that resolves from cache in a few
 *     milliseconds does not flash a bar on screen;
 *   - the bar never reaches 100% until finish(), because a bar that sits at 100%
 *     while the request is still running reads as broken;
 *   - the bar is removed rather than hidden, so it cannot intercept clicks.
 */
export function createProgressBar(): ProgressController {
  let element: HTMLDivElement | null = null
  let startTimer: ReturnType<typeof setTimeout> | null = null
  let removeTimer: ReturnType<typeof setTimeout> | null = null
  let trickleTimer: ReturnType<typeof setInterval> | null = null
  let percent = 0

  const DELAY_BEFORE_SHOWING = 150
  const FADE_OUT = 250

  function ensureStyles(): void {
    if (document.getElementById('vuepher-progress-styles')) {
      return
    }
    const style = document.createElement('style')
    style.id = 'vuepher-progress-styles'
    style.textContent = `
      .vuepher-progress {
        position: fixed;
        top: 0;
        left: 0;
        height: 2px;
        width: 0;
        background: #2563eb;
        z-index: 2147483647;
        transition: width 200ms ease, opacity 250ms ease;
        pointer-events: none;
        opacity: 1;
      }
      @media (prefers-reduced-motion: reduce) {
        .vuepher-progress { transition: none; }
      }
    `
    document.head.appendChild(style)
  }

  function ensureElement(): HTMLDivElement {
    ensureStyles()
    if (element === null) {
      element = document.createElement('div')
      element.className = 'vuepher-progress'
      element.setAttribute('aria-hidden', 'true')
      document.body.appendChild(element)
    }
    return element
  }

  function apply(value: number): void {
    percent = value
    const bar = ensureElement()
    bar.style.width = `${value}%`
    bar.style.opacity = '1'
  }

  return {
    start(): void {
      if (startTimer !== null) {
        clearTimeout(startTimer)
      }
      if (removeTimer !== null) {
        clearTimeout(removeTimer)
        removeTimer = null
      }

      // Do not show anything for a visit that is nearly instantaneous.
      startTimer = setTimeout(() => {
        apply(8)

        // Creep forward while the request is in flight, approaching but never
        // reaching 100%.
        if (trickleTimer !== null) {
          clearInterval(trickleTimer)
        }
        trickleTimer = setInterval(() => {
          const remaining = 90 - percent
          if (remaining <= 0) {
            return
          }
          apply(percent + Math.max(0.5, remaining * 0.12))
        }, 250)
      }, DELAY_BEFORE_SHOWING)
    },

    setPercent(value: number): void {
      if (startTimer !== null) {
        clearTimeout(startTimer)
        startTimer = null
      }
      apply(Math.min(Math.max(value, 0), 99))
    },

    finish(): void {
      if (startTimer !== null) {
        clearTimeout(startTimer)
        startTimer = null
      }
      if (trickleTimer !== null) {
        clearInterval(trickleTimer)
        trickleTimer = null
      }

      if (element === null) {
        // The visit finished before the bar was ever shown: nothing to clean up.
        return
      }

      const bar = element
      bar.style.width = '100%'
      removeTimer = setTimeout(() => {
        bar.style.opacity = '0'
        removeTimer = setTimeout(() => {
          if (bar.parentNode) {
            bar.parentNode.removeChild(bar)
          }
          if (element === bar) {
            element = null
          }
        }, FADE_OUT)
      }, 200)
      percent = 0
    },

    remove(): void {
      if (startTimer !== null) {
        clearTimeout(startTimer)
        startTimer = null
      }
      if (trickleTimer !== null) {
        clearInterval(trickleTimer)
        trickleTimer = null
      }
      if (removeTimer !== null) {
        clearTimeout(removeTimer)
        removeTimer = null
      }
      if (element && element.parentNode) {
        element.parentNode.removeChild(element)
      }
      element = null
      percent = 0
    },
  }
}
