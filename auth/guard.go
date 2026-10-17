package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/vuepher/vuepher"
)

// Authenticatable is a user the guard can sign in (HTTP-09). Application models
// satisfy it with two small methods:
//
//	func (u User) AuthID() string       { return fmt.Sprint(u.ID) }
//	func (u User) AuthPassword() string { return u.Password }
//
// Keeping the contract to two methods means the auth package never has to know
// anything about the application's schema, and a generated user model compiles
// against it unchanged.
type Authenticatable interface {
	// AuthID is the stable identifier stored in the session.
	AuthID() string

	// AuthPassword is the stored password digest. It is only read during an
	// attempt; it must never be sent to the frontend.
	AuthPassword() string
}

// Rememberable is implemented by users that support a "remember me" cookie. It
// is optional: the session guard works without it.
//
// TODO(phase-2): implement the remember-me cookie and its rotation on use.
type Rememberable interface {
	AuthRememberToken() string
	SetAuthRememberToken(string)
}

// Provider resolves users for the guard (HTTP-09). An application typically
// implements it over a generated model:
//
//	type UserProvider struct{ db *orm.DB }
//
//	func (p UserProvider) RetrieveByID(ctx context.Context, id string) (auth.Authenticatable, error) {
//		user, err := orm.QueryOn[models.User](p.db).Find(id)
//		if err != nil {
//			return nil, err
//		}
//		return user, nil
//	}
type Provider interface {
	// RetrieveByID finds a user by the identifier stored in the session.
	// Returning an error for a deleted user is expected and signs the request
	// out.
	RetrieveByID(ctx context.Context, id string) (Authenticatable, error)

	// RetrieveByCredentials finds a user matching the given credentials. Which
	// keys are used is the application's decision (usually email); passwords are
	// verified by the guard, not here.
	RetrieveByCredentials(ctx context.Context, credentials map[string]string) (Authenticatable, error)
}

// SessionGuard signs users in and out and resolves them per request (HTTP-09).
// One instance is shared by the whole application; per-request state lives on
// the RequestGuard it hands out.
type SessionGuard struct {
	provider   Provider
	hasher     vuepher.Hasher
	sessionKey string
}

// DefaultSessionKey is the session key holding the signed-in user's identifier.
const DefaultSessionKey = "auth.id"

// NewSessionGuard builds a guard for a provider.
func NewSessionGuard(provider Provider) *SessionGuard {
	return &SessionGuard{
		provider:   provider,
		hasher:     DefaultHasher(),
		sessionKey: DefaultSessionKey,
	}
}

// WithHasher overrides the password hasher.
func (g *SessionGuard) WithHasher(hasher vuepher.Hasher) *SessionGuard {
	if hasher != nil {
		g.hasher = hasher
	}
	return g
}

// WithSessionKey overrides the session key, for an application that hosts more
// than one guard.
func (g *SessionGuard) WithSessionKey(key string) *SessionGuard {
	if key != "" {
		g.sessionKey = key
	}
	return g
}

// Hasher returns the configured password hasher.
func (g *SessionGuard) Hasher() vuepher.Hasher { return g.hasher }

// ForRequest returns the per-request guard. The user is resolved lazily, so a
// request that never asks about authentication costs no database query.
func (g *SessionGuard) ForRequest(ctx *vuepher.Context) *RequestGuard {
	return &RequestGuard{guard: g, ctx: ctx}
}

// Verify reports whether the password matches the user's stored digest, and
// upgrades the digest when the hashing parameters have since changed.
func (g *SessionGuard) Verify(user Authenticatable, password string) bool {
	if user == nil {
		// Still spend the comparison on a dummy hash so that a missing user and
		// a wrong password take the same time. Otherwise response timing tells
		// an attacker which email addresses exist.
		_ = g.hasher.Verify(password, dummyHash)
		return false
	}
	return g.hasher.Verify(password, user.AuthPassword())
}

// dummyHash is compared against when the user does not exist, to equalise timing.
const dummyHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"

// Attempt signs a user in when the credentials match. It returns the user, or
// nil and ErrInvalidCredentials.
func (g *SessionGuard) Attempt(ctx *vuepher.Context, credentials map[string]string) (Authenticatable, error) {
	return g.AttemptContext(ctx.Context(), ctx, credentials)
}

// AttemptContext is Attempt with an explicit context, so a caller outside a
// request (a login command, a test) can use it.
func (g *SessionGuard) AttemptContext(
	requestCtx context.Context,
	httpCtx *vuepher.Context,
	credentials map[string]string,
) (Authenticatable, error) {
	password := credentials["password"]
	delete(credentials, "password")

	user, err := g.provider.RetrieveByCredentials(requestCtx, credentials)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			// Equalise timing with the "user exists but password is wrong" path.
			_ = g.hasher.Verify(password, dummyHash)
			return nil, ErrInvalidCredentials
		}
		return nil, fmt.Errorf("auth: could not look up the user: %w", err)
	}
	if !g.Verify(user, password) {
		return nil, ErrInvalidCredentials
	}

	if httpCtx != nil {
		if err := g.Login(httpCtx, user); err != nil {
			return nil, err
		}
	}
	return user, nil
}

// Login signs a user in: it stores the identifier in the session and regenerates
// the session identifier so that a session fixed before login cannot be reused
// (§7.4).
func (g *SessionGuard) Login(ctx *vuepher.Context, user Authenticatable) error {
	if user == nil {
		return errors.New("auth: cannot sign in a nil user")
	}
	session := ctx.Session()
	// Regenerating on privilege change is what prevents session fixation
	// (§7.4).
	session.Regenerate()
	// The intended URL is deliberately left in place: it is set by the auth
	// middleware when a page is blocked, and consumed by ctx.Intended() when the
	// handler redirects after a successful sign-in.
	session.Set(g.sessionKey, user.AuthID())
	if guard, ok := ctx.Auth().(*RequestGuard); ok && guard != nil {
		guard.setUser(user)
	}
	return nil
}

// LoginUsingID signs a user in by identifier, for flows that have already
// resolved the user (an invitation link, a test helper).
func (g *SessionGuard) LoginUsingID(ctx *vuepher.Context, id string) (Authenticatable, error) {
	user, err := g.provider.RetrieveByID(ctx.Context(), id)
	if err != nil {
		return nil, fmt.Errorf("auth: could not load user %q: %w", id, err)
	}
	if user == nil {
		return nil, ErrUserNotFound
	}
	return user, g.Login(ctx, user)
}

// Logout signs the current user out and clears the session entirely, so nothing
// from the previous identity survives.
func (g *SessionGuard) Logout(ctx *vuepher.Context) {
	session := ctx.Session()
	session.Remove(g.sessionKey)
	session.Invalidate()
	if guard, ok := ctx.Auth().(*RequestGuard); ok && guard != nil {
		guard.setUser(nil)
	}
}

// deleteExpiredIntended drops a stale intended URL so a user is not bounced to a
// page from an unrelated earlier session.
func deleteExpiredIntended(ctx *vuepher.Context) {
	ctx.Session().Remove("url.intended")
}

// ---------------------------------------------------------------------------
// Per-request guard
// ---------------------------------------------------------------------------

// RequestGuard implements vuepher.Auth for a single request. The user is loaded
// on first access and cached.
type RequestGuard struct {
	guard  *SessionGuard
	ctx    *vuepher.Context
	user   Authenticatable
	loaded bool
	err    error
}

// Check reports whether a user is signed in.
func (r *RequestGuard) Check() bool { return r.resolve() != nil }

// Guest reports whether the request is anonymous.
func (r *RequestGuard) Guest() bool { return !r.Check() }

// ID returns the user's identifier, or nil when anonymous.
func (r *RequestGuard) ID() any {
	user := r.resolve()
	if user == nil {
		return nil
	}
	return user.AuthID()
}

// User returns the signed-in user, or nil.
func (r *RequestGuard) User() any {
	user := r.resolve()
	if user == nil {
		// Return an untyped nil so a handler comparing against nil is not fooled
		// by a non-nil interface holding a nil pointer.
		return nil
	}
	return user
}

// Err returns the error encountered while loading the user, if any. A user that
// no longer exists counts as anonymous rather than as an error.
func (r *RequestGuard) Err() error { return r.err }

// resolve loads the user from the session once.
func (r *RequestGuard) resolve() Authenticatable {
	if r.loaded {
		return r.user
	}
	r.loaded = true

	session := r.ctx.Session()
	raw := session.GetString(r.guard.sessionKey, "")
	if raw == "" {
		return nil
	}

	user, err := r.guard.provider.RetrieveByID(r.ctx.Context(), raw)
	if err != nil {
		// The session references a user that is gone (deleted account, changed
		// identifier). Treat it as a guest and drop the stale session value.
		if !errors.Is(err, ErrUserNotFound) {
			r.err = err
			if r.ctx.App() != nil {
				r.ctx.App().Logger.Warn("auth: session referenced an unusable user", "id", raw, "error", err)
			}
		}
		session.Remove(r.guard.sessionKey)
		return nil
	}
	r.user = user
	return r.user
}

// setUser installs a user without a database round trip, used right after a
// successful login or logout within the same request.
func (r *RequestGuard) setUser(user Authenticatable) {
	r.loaded = true
	r.user = user
}

// UserAs is the typed accessor: it returns the signed-in user as the concrete
// model type, so handlers avoid a type assertion.
//
//	user, ok := auth.UserAs[*models.User](ctx)
func UserAs[T any](ctx *vuepher.Context) (T, bool) {
	var zero T
	guard, ok := ctx.Auth().(*RequestGuard)
	if !ok || guard == nil {
		return zero, false
	}
	user := guard.resolve()
	if user == nil {
		return zero, false
	}
	typed, ok := user.(T)
	return typed, ok
}

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

// ErrInvalidCredentials is returned when the credentials do not match. It gives
// no indication which part was wrong, deliberately: telling an attacker whether
// an email exists is a user-enumeration vector.
var ErrInvalidCredentials = errors.New("auth: these credentials do not match our records")

// ErrUserNotFound is returned by a provider when no user matches. Providers
// should return it rather than a database error so the guard can equalise
// timing.
var ErrUserNotFound = errors.New("auth: user not found")

// ErrUnauthenticated is returned by the middleware when authentication is
// required and absent.
var ErrUnauthenticated = errors.New("auth: authentication is required")
