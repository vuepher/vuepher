# Security policy

## Reporting a vulnerability

Do not open a public issue for a security problem.

Report it privately through GitHub's "Report a vulnerability" form on the
repository (Security → Advisories → Report a vulnerability), or by email to
`security@vuepher.dev`.

Please include:

- the version (`vuepher version`) and platform;
- a minimal reproduction, or the exact request that triggers it;
- what an attacker gains, and what they need to reach the code path;
- whether the report is already public elsewhere.

We aim to acknowledge within 3 working days and to ship a fix for high-severity
issues within 30 days. We will credit you in the advisory unless you ask us not
to.

## Supported versions

Vuepher is pre-1.0: only the latest release is supported. Security fixes are
shipped as patch releases, and breaking changes are reserved for minor releases
before 1.0 and major releases after it.

## What the framework handles by default

These are framework guarantees, not suggestions — a report that one of them does
not hold is a valid security report.

| Area | Behaviour |
| --- | --- |
| CSRF | On by default for state-changing methods (`middleware.CSRF`), token in the session, compared in constant time. |
| XSS | The page object embedded in `data-page` is HTML-entity escaped for attribute context; a prop value cannot close the attribute or inject markup. |
| Session cookies | `HttpOnly`, `SameSite=Lax` and `Path=/` always; `Secure` in production. |
| Session fixation | `middleware.StartSession` regenerates the identifier on privilege change. |
| Password hashing | bcrypt or argon2id through `auth.DefaultHasher()`. |
| Validation | Runs on the server, in Go, so it also protects non-browser clients. A validation failure never leaks internal detail: the client gets the flashed messages, not a stack trace. |
| Redirects | `ctx.Back()` follows a same-origin referer only, so it cannot be turned into an open redirect. |
| Error pages | `APP_DEBUG=false` in production suppresses internal error detail from error pages. |
| SQL | The fluent query builder binds parameters; identifiers passed to `OrderBy`/`Select` are validated against a whitelist pattern. |
| Assets | Content-hashed filenames served with immutable caching; no source maps in production. |

## What is the application's responsibility

- Protecting routes: register `auth.Middleware(guard)` and use the `auth` /
  `guest` middleware on the routes that need it. The framework does not guess
  which pages require a login.
- Authorisation: a session guard authenticates a user, it does not decide whether
  that user may edit *this* post. Check that in the controller.
- Secrets: `APP_KEY` and database credentials belong in `.env` (git-ignored) or
  the process environment, never in `config/*.go`.
- `APP_DEBUG=false` and `APP_ENV=production` in production, so cookies get
  `Secure` and error pages stop including internal detail.
- Writing raw SQL deliberately: `db.Raw(...)` is an escape hatch, not a place to
  interpolate user input into a string.

## Dependency hygiene

`govulncheck` and `npm audit` run in CI (`make verify` covers the local
equivalent). New dependencies need a justification in the pull request — see
`CONTRIBUTING.md`. The framework's runtime dependency set is intentionally small:
Chi, GORM with its drivers, the Go playground validator, `godotenv`, `uuid` and
the stub/CLI packages.
