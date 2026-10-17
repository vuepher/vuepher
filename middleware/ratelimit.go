package middleware

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vuepher/vuepher"
)

// RateLimitOptions configure rate limiting (HTTP-05).
type RateLimitOptions struct {
	// Max is the number of requests allowed per window.
	Max int

	// Window is the length of the window. Defaults to one minute.
	Window time.Duration

	// Key identifies the client. Defaults to the remote IP, honouring
	// X-Forwarded-For when TrustProxy is set. Override it to limit per user:
	//
	//	Key: func(ctx *vuepher.Context) string {
	//		if guard := ctx.Auth(); guard != nil && guard.Check() {
	//			return fmt.Sprintf("user:%v", guard.ID())
	//		}
	//		return ctx.Request().RemoteAddr
	//	}
	Key func(*vuepher.Context) string

	// TrustProxy makes the client IP come from X-Forwarded-For or X-Real-IP.
	// Only enable it behind a proxy you control, otherwise a client can spoof
	// its address and evade the limit.
	TrustProxy bool

	// Store defaults to an in-process store. Supply a shared store when running
	// more than one instance, otherwise each replica applies its own limit.
	Store RateLimitStore

	// Message is the body of a 429 response.
	Message string
}

// RateLimitStore counts requests per key. Implement it over Redis to share a
// limit across instances.
type RateLimitStore interface {
	// Hit records a request for key and reports the count within the current
	// window plus when the window resets.
	Hit(key string, window time.Duration) (count int, resetAt time.Time)
}

// RateLimit rejects requests once a client exceeds the configured rate
// (HTTP-05).
//
// The response is 429 with Retry-After and the standard rate-limit headers, so a
// well-behaved client can back off without guessing.
func RateLimit(options RateLimitOptions) vuepher.MiddlewareFunc {
	if options.Max <= 0 {
		options.Max = 60
	}
	if options.Window <= 0 {
		options.Window = time.Minute
	}
	if options.Store == nil {
		options.Store = NewMemoryRateLimitStore()
	}
	if options.Message == "" {
		options.Message = "Too many requests. Please slow down and try again shortly."
	}
	keyFn := options.Key
	if keyFn == nil {
		keyFn = func(ctx *vuepher.Context) string {
			return clientIP(ctx, options.TrustProxy)
		}
	}

	return func(ctx *vuepher.Context, next vuepher.HandlerFunc) error {
		key := keyFn(ctx)
		count, resetAt := options.Store.Hit(key, options.Window)

		remaining := options.Max - count
		if remaining < 0 {
			remaining = 0
		}
		header := ctx.Response().Header()
		header.Set("X-RateLimit-Limit", strconv.Itoa(options.Max))
		header.Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
		header.Set("X-RateLimit-Reset", strconv.FormatInt(resetAt.Unix(), 10))

		if count > options.Max {
			retry := int(time.Until(resetAt).Seconds())
			if retry < 1 {
				retry = 1
			}
			header.Set("Retry-After", strconv.Itoa(retry))
			return vuepher.Abort(http.StatusTooManyRequests, options.Message)
		}
		return next(ctx)
	}
}

// clientIP resolves the client address, optionally from proxy headers.
func clientIP(ctx *vuepher.Context, trustProxy bool) string {
	if trustProxy {
		if forwarded := ctx.Header("X-Forwarded-For"); forwarded != "" {
			// The left-most entry is the original client.
			if idx := strings.Index(forwarded, ","); idx > 0 {
				return strings.TrimSpace(forwarded[:idx])
			}
			return strings.TrimSpace(forwarded)
		}
		if realIP := ctx.Header("X-Real-IP"); realIP != "" {
			return realIP
		}
	}
	host, _, err := net.SplitHostPort(ctx.Request().RemoteAddr)
	if err != nil {
		return ctx.Request().RemoteAddr
	}
	return host
}

// ---------------------------------------------------------------------------
// In-process store
// ---------------------------------------------------------------------------

// MemoryRateLimitStore is a fixed-window counter kept in process memory. One
// mutex-guarded map is enough for a single instance; it deliberately does not
// try to be clever, because a production deployment that needs a shared limit
// needs Redis anyway.
type MemoryRateLimitStore struct {
	mu      sync.Mutex
	buckets map[string]*rateBucket

	// sweepEvery controls how often expired buckets are dropped, to stop the
	// map growing without bound under a scanned-IP attack.
	sweepEvery time.Duration
	lastSweep  time.Time
}

type rateBucket struct {
	count   int
	resetAt time.Time
}

// NewMemoryRateLimitStore builds an in-process store.
func NewMemoryRateLimitStore() *MemoryRateLimitStore {
	return &MemoryRateLimitStore{
		buckets:    map[string]*rateBucket{},
		sweepEvery: time.Minute,
		lastSweep:  time.Now(),
	}
}

// Hit implements RateLimitStore.
func (s *MemoryRateLimitStore) Hit(key string, window time.Duration) (int, time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	if now.Sub(s.lastSweep) > s.sweepEvery {
		for k, bucket := range s.buckets {
			if now.After(bucket.resetAt) {
				delete(s.buckets, k)
			}
		}
		s.lastSweep = now
	}

	bucket, ok := s.buckets[key]
	if !ok || now.After(bucket.resetAt) {
		bucket = &rateBucket{count: 0, resetAt: now.Add(window)}
		s.buckets[key] = bucket
	}
	bucket.count++
	return bucket.count, bucket.resetAt
}

// Reset clears the counters, for tests.
func (s *MemoryRateLimitStore) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buckets = map[string]*rateBucket{}
}

// Size reports how many clients are currently tracked.
func (s *MemoryRateLimitStore) Size() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.buckets)
}

// Throttle is the conventional Laravel name for RateLimit, kept as an alias so
// that migration documentation reads naturally.
func Throttle(max int, window time.Duration) vuepher.MiddlewareFunc {
	return RateLimit(RateLimitOptions{Max: max, Window: window})
}

// RateLimitKeyFor builds a key function that limits per authenticated user and
// falls back to the IP for guests (HTTP-05).
func RateLimitKeyFor(prefix string) func(*vuepher.Context) string {
	return func(ctx *vuepher.Context) string {
		if guard := ctx.Auth(); guard != nil && guard.Check() {
			return fmt.Sprintf("%s:user:%v", prefix, guard.ID())
		}
		return fmt.Sprintf("%s:ip:%s", prefix, clientIP(ctx, false))
	}
}
