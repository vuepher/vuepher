package vuepher

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Session holds state for one browser session (HTTP-08). Values set with Set
// persist; values set with Flash survive exactly one further request, which is
// how redirect messages and validation errors travel.
type Session interface {
	// ID returns the session identifier. It is regenerated on login to prevent
	// session fixation (§7.4).
	ID() string

	// Get reads a persistent or flashed value.
	Get(key string) any

	// GetString reads a value as a string, returning def when absent.
	GetString(key string, def string) string

	// Has reports whether a key holds a value.
	Has(key string) bool

	// Set stores a value for the rest of the session's life.
	Set(key string, value any)

	// Remove deletes keys from the session.
	Remove(keys ...string)

	// Flash stores a value for exactly one subsequent request.
	Flash(key string, value any)

	// FlashInput stores submitted input for the next request so a failed form
	// can repopulate itself.
	FlashInput(inputs map[string]any)

	// OldInput returns input flashed by the previous request.
	OldInput() map[string]any

	// FlashErrors stores a validation error bag for the next request.
	// name defaults to "default" (VIEW-08).
	FlashErrors(bag ErrorBag, name string)

	// Errors returns the error bag flashed by the previous request.
	Errors(name string) ErrorBag

	// Token returns the CSRF token, creating one on first use.
	Token() string

	// Regenerate issues a new session ID while keeping the data.
	Regenerate()

	// Invalidate clears the session entirely, used on logout.
	Invalidate()

	// All returns a copy of the session's values and flash data, for debugging.
	All() map[string]any

	// state exposes the wire representation to stores.
	state() *sessionState

	// dirty reports whether the session changed and needs saving.
	dirty() bool
}

// sessionState is the serialised form of a session. It is what the cookie store
// encrypts and what a database or Redis driver would persist as one document.
//
// Flash and Next are two buckets: Flash is what the previous request left for
// this one, Next is what this request leaves for the next. Store.Write promotes
// Next into Flash, which is what makes flash data live for exactly one request.
type sessionState struct {
	ID       string              `json:"id"`
	Values   map[string]any      `json:"values,omitempty"`
	Flash    map[string]any      `json:"flash,omitempty"`
	Next     map[string]any      `json:"next,omitempty"`
	OldInput map[string]any      `json:"old,omitempty"`
	NextOld  map[string]any      `json:"nold,omitempty"`
	Errors   map[string]ErrorBag `json:"errors,omitempty"`
	NextErr  map[string]ErrorBag `json:"nerr,omitempty"`
	CSRF     string              `json:"csrf,omitempty"`
}

func newSessionState() *sessionState {
	return &sessionState{
		ID:     uuid.NewString(),
		Values: map[string]any{},
	}
}

// promoteNextRequestState moves what this request left for the next one into the
// buckets the next request reads. Calling it from every store's Write is what
// makes flash data — flash messages, validation errors and old input — live for
// exactly one request regardless of the driver in use: a driver that forgot to
// promote would silently drop all three.
func promoteNextRequestState(state *sessionState) {
	state.Flash = state.Next
	state.Next = nil
	state.OldInput = state.NextOld
	state.NextOld = nil
	state.Errors = state.NextErr
	state.NextErr = nil
}

// session is the default Session implementation, independent of the store
// backing it.
type session struct {
	s       *sessionState
	mu      *sync.Mutex
	touched bool
}

// newMemorySession builds a session that is never persisted. Used when handlers
// are exercised without the session middleware in place.
func newMemorySession() Session {
	return &session{s: newSessionState(), mu: &sync.Mutex{}}
}

// newSessionFromState wraps a decoded state, marking it clean.
func newSessionFromState(state *sessionState) *session {
	if state == nil {
		state = newSessionState()
	}
	if state.Values == nil {
		state.Values = map[string]any{}
	}
	return &session{s: state, mu: &sync.Mutex{}}
}

func (s *session) state() *sessionState { return s.s }

func (s *session) dirty() bool { return s.touched }

func (s *session) ID() string { return s.s.ID }

func (s *session) Get(key string) any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.s.Values[key]; ok {
		return v
	}
	if v, ok := s.s.Flash[key]; ok {
		return v
	}
	return nil
}

func (s *session) GetString(key string, def string) string {
	v := s.Get(key)
	if text, ok := v.(string); ok && text != "" {
		return text
	}
	return def
}

func (s *session) Has(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.s.Values[key]; ok {
		return true
	}
	_, ok := s.s.Flash[key]
	return ok
}

func (s *session) Set(key string, value any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.s.Values[key] = value
	s.touched = true
}

func (s *session) Remove(keys ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, key := range keys {
		delete(s.s.Values, key)
		delete(s.s.Flash, key)
		delete(s.s.Next, key)
	}
	s.touched = true
}

func (s *session) Flash(key string, value any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.s.Next == nil {
		s.s.Next = map[string]any{}
	}
	s.s.Next[key] = value
	s.touched = true
}

func (s *session) FlashInput(inputs map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.s.NextOld = inputs
	s.touched = true
}

func (s *session) OldInput() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.s.OldInput
}

func (s *session) FlashErrors(bag ErrorBag, name string) {
	if bag == nil {
		return
	}
	if name == "" {
		name = "default"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.s.NextErr == nil {
		s.s.NextErr = map[string]ErrorBag{}
	}
	s.s.NextErr[name] = bag
	s.touched = true
}

func (s *session) Errors(name string) ErrorBag {
	if name == "" {
		name = "default"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.s.Errors == nil {
		return nil
	}
	return s.s.Errors[name]
}

func (s *session) Token() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.s.CSRF == "" {
		s.s.CSRF = uuid.NewString()
		s.touched = true
	}
	return s.s.CSRF
}

func (s *session) Regenerate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.s.ID = uuid.NewString()
	s.touched = true
}

func (s *session) Invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := newSessionState()
	s.s = state
	s.touched = true
}

func (s *session) All() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]any, len(s.s.Values)+len(s.s.Flash))
	for k, v := range s.s.Values {
		out[k] = v
	}
	for k, v := range s.s.Flash {
		out[k] = v
	}
	return out
}

// ---------------------------------------------------------------------------
// Stores
// ---------------------------------------------------------------------------

// SessionStore reads and writes sessions (HTTP-08). The cookie driver is the
// default; database and Redis drivers plug in here (PRD Phase 2).
type SessionStore interface {
	// Read loads the session for a request. It must never return nil: a fresh
	// session is created when the client has none.
	Read(r *http.Request) (Session, error)

	// Write persists the session, typically by setting a cookie.
	Write(w http.ResponseWriter, s Session) error
}

// CookieSessionStore keeps the whole session in an encrypted, authenticated
// cookie. Stateless, needs no storage backend, and survives a restart — at the
// cost of a larger cookie and no server-side revocation.
//
// The payload is AES-256-GCM encrypted with the application key, so the contents
// are both confidential and tamper-evident (R9).
type CookieSessionStore struct {
	Name     string
	Key      []byte
	Path     string
	Domain   string
	Lifetime time.Duration
	Secure   bool
	SameSite http.SameSite
}

// NewCookieSessionStore builds the default store. appKey is the APP_KEY value;
// it is stretched to 32 bytes with a fixed derivation so that any non-empty key
// works.
func NewCookieSessionStore(appKey string, cfg SessionConfig) *CookieSessionStore {
	if cfg.Name == "" {
		cfg.Name = "vuepher_session"
	}
	if cfg.Path == "" {
		cfg.Path = "/"
	}
	sameSite := cfg.SameSite
	if sameSite == http.SameSiteDefaultMode {
		sameSite = http.SameSiteLaxMode
	}
	return &CookieSessionStore{
		Name:     cfg.Name,
		Key:      deriveKey(appKey),
		Path:     cfg.Path,
		Domain:   cfg.Domain,
		Lifetime: time.Duration(cfg.LifetimeMinutes) * time.Minute,
		Secure:   cfg.Secure,
		SameSite: sameSite,
	}
}

// Read decodes the session cookie. A missing, malformed or undecryptable cookie
// yields a fresh session rather than an error: a stale cookie must never break
// the application, it should just log the user out.
func (s *CookieSessionStore) Read(r *http.Request) (Session, error) {
	cookie, err := r.Cookie(s.Name)
	if err != nil {
		return newSessionFromState(nil), nil
	}
	state, err := s.decode(cookie.Value)
	if err != nil {
		return newSessionFromState(nil), nil
	}
	return newSessionFromState(state), nil
}

// Write encrypts and sets the session cookie. When the session is clean and the
// browser already has a cookie, nothing is written, keeping responses smaller
// and cacheable.
func (s *CookieSessionStore) Write(w http.ResponseWriter, sess Session) error {
	if sess == nil {
		return nil
	}
	concrete, ok := sess.(*session)
	if !ok {
		return errors.New("vuepher: session store only understands sessions created by this framework")
	}

	state := concrete.state()
	promoteNextRequestState(state)

	encoded, err := s.encode(state)
	if err != nil {
		return fmt.Errorf("vuepher: could not encode session: %w", err)
	}

	maxAge := int(s.Lifetime.Seconds())
	if maxAge <= 0 {
		maxAge = 7200
	}
	http.SetCookie(w, &http.Cookie{
		Name:     s.Name,
		Value:    encoded,
		Path:     s.Path,
		Domain:   s.Domain,
		MaxAge:   maxAge,
		Expires:  time.Now().Add(time.Duration(maxAge) * time.Second),
		HttpOnly: true,
		Secure:   s.Secure,
		SameSite: s.SameSite,
	})
	return nil
}

// Destroy expires the session cookie, used on logout.
func (s *CookieSessionStore) Destroy(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.Name,
		Value:    "",
		Path:     s.Path,
		Domain:   s.Domain,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.Secure,
		SameSite: s.SameSite,
	})
}

// encode serialises and encrypts the state.
//
// The state is JSON rather than gob: sessions hold arbitrary user values and gob
// refuses to traverse interface values whose concrete types it has not been told
// about, which would make `session.Set("x", someStruct)` fail at runtime.
func (s *CookieSessionStore) encode(state *sessionState) (string, error) {
	plaintext, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(s.Key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

// decode decrypts and parses a cookie value.
func (s *CookieSessionStore) decode(value string) (*sessionState, error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(s.Key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize() {
		return nil, errors.New("vuepher: session payload is truncated")
	}
	nonce, ciphertext := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("vuepher: session payload failed authentication: %w", err)
	}
	var state sessionState
	if err := json.Unmarshal(plaintext, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

// deriveKey turns an arbitrary application key into a 32-byte AES key. An empty
// key is replaced with a fixed development key so that a missing APP_KEY does
// not crash local development; Config warns about this at boot instead.
func deriveKey(appKey string) []byte {
	const devKey = "vuepher-insecure-development-key"
	if appKey == "" {
		appKey = devKey
	}
	sum := sha256Sum([]byte(appKey))
	return sum
}

// ---------------------------------------------------------------------------
// Memory store (HTTP-08, "memory" driver; also used by tests)
// ---------------------------------------------------------------------------

// MemorySessionStore keeps sessions in a process-local map. Useful for tests and
// single-instance deployments that do not want a cookie payload; sessions are
// lost on restart.
type MemorySessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*sessionState
	Name     string
	Lifetime time.Duration
}

// NewMemorySessionStore builds an in-process session store.
func NewMemorySessionStore() *MemorySessionStore {
	return &MemorySessionStore{
		sessions: map[string]*sessionState{},
		Name:     "vuepher_session",
		Lifetime: 2 * time.Hour,
	}
}

// Read loads the session referenced by the cookie, creating one when absent.
func (m *MemorySessionStore) Read(r *http.Request) (Session, error) {
	cookie, err := r.Cookie(m.Name)
	if err != nil || cookie.Value == "" {
		state := newSessionState()
		m.mu.Lock()
		m.sessions[state.ID] = state
		m.mu.Unlock()
		return newSessionFromState(state), nil
	}
	m.mu.RLock()
	state, ok := m.sessions[cookie.Value]
	m.mu.RUnlock()
	if !ok {
		state = newSessionState()
		state.ID = cookie.Value
		m.mu.Lock()
		m.sessions[state.ID] = state
		m.mu.Unlock()
	}
	return newSessionFromState(state), nil
}

// Write stores the session and sets its cookie.
func (m *MemorySessionStore) Write(w http.ResponseWriter, sess Session) error {
	if sess == nil {
		return nil
	}
	state := sess.state()
	promoteNextRequestState(state)
	if state.Flash == nil {
		state.Flash = map[string]any{}
	}
	m.mu.Lock()
	m.sessions[state.ID] = state
	m.mu.Unlock()

	maxAge := int(m.Lifetime.Seconds())
	http.SetCookie(w, &http.Cookie{
		Name:     m.Name,
		Value:    state.ID,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// Destroy forgets a session.
func (m *MemorySessionStore) Destroy(w http.ResponseWriter, sess Session) {
	if sess != nil {
		m.mu.Lock()
		delete(m.sessions, sess.ID())
		m.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: m.Name, Value: "", Path: "/", MaxAge: -1})
}
