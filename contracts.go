package vuepher

// Auth is the authentication contract the core needs (HTTP-09). The concrete
// guard lives in the auth package; defining the interface here keeps the core
// free of an import cycle and lets an application substitute its own guard.
//
// Usage inside a handler:
//
//	if ctx.Auth().Guest() {
//		return ctx.RedirectRoute("login")
//	}
//	user := ctx.Auth().User()
type Auth interface {
	// Check reports whether the request is authenticated.
	Check() bool

	// Guest is the inverse of Check, for readability in conditionals.
	Guest() bool

	// ID returns the authenticated user's identifier, or nil.
	ID() any

	// User returns the authenticated user, or nil.
	User() any
}

// Hasher hashes and verifies passwords (HTTP-09). bcrypt and argon2id are
// provided by the auth package.
type Hasher interface {
	// Hash returns an encoded hash suitable for storing in the database.
	Hash(plain string) (string, error)

	// Verify reports whether plain matches the encoded hash. It must use a
	// constant-time comparison.
	Verify(plain, encoded string) bool

	// NeedsRehash reports whether encoded was produced with parameters weaker
	// than the current configuration, so a login can transparently upgrade it.
	NeedsRehash(encoded string) bool
}
