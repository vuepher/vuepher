# The view bridge: Go ↔ Vue protocol

Status: **specification, protocol version 1.0** (see `vuepher.ProtocolVersion`).

This document is the contract between the Go server and the `@vuepher/vue3`
client. It is written out explicitly because the protocol is the framework's
differentiator *and* its biggest compatibility risk (PRD risk R4): two
implementations in two languages have to agree on every byte, and an implicit
contract drifts.

The protocol is conceptually compatible with Inertia.js — the mental model moves
over intact — but it is Vuepher's own specification. It is **not** claimed to be
wire-compatible with the official Inertia adapters; the divergences are listed at
the end.

Every rule below is covered by a test in `tests/protocol_test.go`.

---

## 1. Actors and the shape of a request

```
Browser ──▶ Vue 3 app (@vuepher/vue3)
                 │  first visit:  full page load
                 │  afterwards:   fetch() with X-Vuepher headers
                 ▼
            Go router ──▶ middleware ──▶ controller
                                            │
                                    ctx.View(component, props)
                                            │
                          HTML document  or  JSON page object
```

There is no REST API in between. A controller decides what the page is; the
client decides how to render it.

## 2. Request headers

| Header | Set by | Meaning |
| --- | --- | --- |
| `X-Vuepher` | client | Presence (any value) marks an SPA visit: answer with a **JSON page object**, not a document. |
| `X-Vuepher-Version` | client | The asset version the client currently holds. A mismatch is answered with `409` (VIEW-04). |
| `X-Vuepher-Partial-Data` | client | `true` requests a partial reload, narrowed by the two headers below (VIEW-06). |
| `X-Vuepher-Partial-Component` | client | The component the client is on right now. A partial reload addressed at a *different* component is served in full. |
| `X-Vuepher-Partial-Only` | client | Comma-separated prop names to include. Empty means "all". |
| `X-Vuepher-Partial-Except` | client | Comma-separated prop names to omit. Ignored when `Partial-Only` is present. |
| `X-Vuepher-Error-Bag` | client | Names the validation error bag to expose as `props.errors` (VIEW-08). Empty means the default bag. |

## 3. Response headers

| Header | Status | Meaning |
| --- | --- | --- |
| `Content-Type: text/html; charset=utf-8` | 200 | A full document. |
| `Content-Type: application/json; charset=utf-8` | 200 | A page object. |
| `X-Vuepher-Location` | 409 | Where the client should perform a full page load. |
| `Location` | 302 / 303 | An ordinary redirect. |

## 4. The page object

```jsonc
{
  "component": "Posts/Index",   // Vue component to resolve
  "props": { "posts": [/* … */], "flash": {}, "errors": {}, "old": {} },
  "url": "/posts?page=2",       // current URL, query string included
  "version": "9d1f0c7a4b2e5f81",// asset version of the build that produced this
  "deferredProps": ["comments"],// props resolved lazily on this request (optional)
  "mergeProps": [],             // reserved (optional)
  "clearHistory": false,        // reserved (optional)
  "encryptHistory": false       // reserved (optional)
}
```

`component` is resolved by the client through `import.meta.glob` over
`resources/js/Pages`. A name the client cannot resolve is a client-side error;
the server never inspects the filesystem to validate it.

`props` always contains the shared props injected by the `ShareProps`
middleware — `flash`, `errors`, `old`, `auth` and `csrf_token` — merged with the
props the controller passed. A controller therefore never mentions flash data or
the authenticated user.

`version` is the hash of the Vite build manifest, so deploying new assets
invalidates every open tab's client cache. It is empty when no manifest exists
(frontend not built yet).

## 5. First visit: the HTML document

The renderer answers with the root template (`resources/views/app.html`) with
these placeholders substituted:

| Placeholder | Required | Contents |
| --- | --- | --- |
| `{{ .VuepherApp }}` | yes | `<div id="app" data-page="…">` with the page object embedded |
| `{{ .VuepherAssets }}` | yes | Every style and script tag |
| `{{ .VuepherStyleTags }}` | no | Stylesheet tags, for `<head>` |
| `{{ .VuepherScriptTags }}` | no | Script tags, for the end of `<body>` |
| `{{ .VuepherVersion }}` | no | The asset version |
| `{{ .VuepherComponent }}` | no | The component being rendered |
| `{{ .Title }}` | no | The document title |
| `{{ .VuepherLang }}` | no | The `lang` attribute |

The client reads the page object with
`JSON.parse(document.getElementById('app').dataset.page)`.

### 5.1 Escaping rules (important)

`data-page` holds JSON inside a double-quoted HTML attribute, so the two escape
grammars have to compose correctly:

1. `encoding/json` produces the JSON text.
2. Every `&`, `<`, `>`, `"`, `'` is then replaced with an **HTML entity**
   (`&amp;`, `&lt;`, `&gt;`, `&quot;`, `&#39;`) in a single pass.
3. The result is inserted as the attribute value.

The browser's HTML parser decodes the entities when it builds the DOM, so
`dataset.page` is the original JSON again and `JSON.parse` succeeds. This is the
only scheme that survives the round trip.

Escaping with JSON escapes instead (`\"` → `\u0022`) looks equivalent and is
not: the escape lands at structural positions too, where JSON has no escape
sequence, and the client fails with *"Expected property name or '}'"*. The
framework did exactly that until a contract test caught it.

A JSON payload must never be HTML-escaped *after* being inserted, and the
attribute must never be marked unsafe, or the escaping happens twice and the
client receives `&amp;quot;`.

## 6. Redirects

| Situation | Status | Notes |
| --- | --- | --- |
| `ctx.Redirect(...)` / `ctx.Back()` on a `GET` | `302` | Ordinary redirect. |
| The same on `POST`/`PUT`/`PATCH`/`DELETE` | `303` | Required by PRD step 6: after a write the browser must repeat the request as a `GET`, otherwise a refresh resubmits the form. |
| `ctx.Location(externalURL)` on an SPA visit | `409` + `X-Vuepher-Location` | The URL is outside the app, so the client must leave the SPA. |

Flash data travels on the redirect response's session cookie: `Redirect().With()`
sets a flash message, `WithErrors()` a validation bag, and the submitted input is
flashed automatically so a failed form repopulates itself. The next request sees
them as `props.flash`, `props.errors` and `props.old`.

Flash values are promoted to the next-request buckets *by the session store's
`Write`*, so every driver must do it. A driver that forgets drops flash messages,
validation errors and old input together.

## 7. Validation

Validation never produces a JSON error body. A failed `ctx.Validate(&req)`
returns an `ErrorBag`, and the controller answers with `ctx.BackWithErrors(err)`:
a `303` back to the referer carrying the bag and the input on the session. The
next page renders with `props.errors` populated, which `useVuepherForm()` binds
automatically.

`props.errors` is always an object, never `null`, so `form.errors.title` is
always safe to read. Named bags (VIEW-08) are selected with the
`X-Vuepher-Error-Bag` request header.

Body parsing is identical for form posts and JSON bodies, including ignoring
unknown keys: a client that sends one extra field gets a validation error it can
act on, not an opaque decode failure.

## 8. Asset versioning and `409`

1. The client stores `version` from the page object it was given.
2. Every subsequent SPA visit sends it as `X-Vuepher-Version`.
3. If the server's current build differs, the server answers **`409 Conflict`**
   with `X-Vuepher-Location: <current URL>` and an empty body.
4. The client performs a full page load of that URL.

This is what makes a deploy safe while tabs are open: the client never keeps
rendering components from a build that no longer exists.

## 9. Partial reloads

A partial reload re-fetches *some* props for the *current* component:

```
X-Vuepher: true
X-Vuepher-Partial-Data: true
X-Vuepher-Partial-Component: Posts/Index
X-Vuepher-Partial-Only: posts
```

Rules:

- Applied only when `Partials-Component` matches the component being rendered.
  When it does not — a redirect landed the user somewhere they did not ask for —
  the response is a normal full page.
- `Only` wins over `Except`.
- Filtering happens after shared props are merged, so `errors` and `flash` are
  dropped unless they are requested. This is deliberate: the client keeps the
  props it already holds for keys that were not sent.
- Non-matching keys are omitted rather than sent as `null`, so the client can
  tell "not requested" from "explicitly empty".

Deferred props (`deferredProps`) are resolved server-side before the merge, and
the response tells the client which ones arrived.

## 10. Errors

| Status | Component | Notes |
| --- | --- | --- |
| `403`, `404`, `429`, `503` | `Errors/403`, `Errors/404`, … | Configurable per application with `Renderer.SetErrorComponent`. |
| any other `4xx` | the `404` page | "What you asked for is not here" reads better than "the server broke". |
| `5xx` | `Errors/500` | With `APP_DEBUG=true`, `props.debug` carries the formatted error. |

Error pages are ordinary Vue components receiving at least:

```jsonc
{ "status": 404, "message": "Page not found." }
```

**An error response always renders a full HTML document, even for an SPA
visit.** The error may have been raised before a page object was possible (an
unmatched route, a panic in a middleware), and a JSON body the client cannot
mount would leave a blank screen. The client's rule is: a JSON
`Content-Type` means swap the page; anything else means full reload.

Unmatched routes and methods are routed through the application error path, so
`chi`'s plain-text 404/405 never reaches the browser.

## 11. Compatibility and versioning

- `vuepher.ProtocolVersion` ("1.0") versions the wire shape, independently of
  the framework version. Changing the page object's fields, the header names or
  the status-code rules is a **breaking change**: it requires a bump and a
  matching release of `@vuepher/vue3`.
- The framework refuses nothing on protocol grounds: an old client sees a 409 and
  reloads, which recovers from most mismatches.
- Contract tests in `tests/protocol_test.go` build a real application, serve it
  over `httptest` and assert this document's rules, including the escaping round
  trip, the `409` body-less response and the partial-reload filtering.

### Divergences from Inertia.js

| Area | Inertia.js | Vuepher |
| --- | --- | --- |
| Headers | `X-Inertia*` | `X-Vuepher*` |
| Error pages | adapter-dependent | always a full document, always a Vue component |
| Error bags | `errorBag` prop | request header + `errors` prop, with named bags |
| Partial reload filtering | after merge | after merge (same) |
| Version mismatch | `409` + `X-Inertia-Location` | `409` + `X-Vuepher-Location` (same shape) |

The header prefix is deliberate: adopting the `X-Inertia*` names would imply
plugin and adapter compatibility that has not been tested.
