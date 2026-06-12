package server

import (
	"net"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"
)

// maxConcurrentBcrypt sizes the bcrypt verification semaphore. bcrypt is
// CPU-bound and deliberately slow, so we cap concurrency at the number of
// usable cores (min 1) to keep a login burst from starving every request
// of CPU.
func maxConcurrentBcrypt() int {
	n := runtime.GOMAXPROCS(0)
	if n < 1 {
		n = 1
	}
	return n
}

// clientIP extracts a best-effort client identifier for rate limiting.
// The host strips/normalises forwarding headers before reaching the
// plugin, but we still prefer the left-most X-Forwarded-For hop when
// present (reverse-proxy deployments) and fall back to RemoteAddr.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if first := strings.TrimSpace(strings.Split(xff, ",")[0]); first != "" {
			return first
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}

// ipRateLimiter is a fixed-window per-IP request counter. It is cheap
// and bounded: stale buckets are swept lazily on access. Used to throttle
// the public catalog/stats endpoints so a single client can't flood the
// DB-backed read paths.
type ipRateLimiter struct {
	mu       sync.Mutex
	window   time.Duration
	limit    int
	buckets  map[string]*rateBucket
	lastSwep time.Time
}

type rateBucket struct {
	count       int
	windowStart time.Time
}

func newIPRateLimiter(limit int, window time.Duration) *ipRateLimiter {
	return &ipRateLimiter{
		window:   window,
		limit:    limit,
		buckets:  map[string]*rateBucket{},
		lastSwep: time.Now(),
	}
}

// allow reports whether a request from ip is permitted in the current
// window and increments the counter when it is.
func (l *ipRateLimiter) allow(ip string) bool {
	if l == nil {
		return true
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweepLocked(now)
	b, ok := l.buckets[ip]
	if !ok || now.Sub(b.windowStart) >= l.window {
		l.buckets[ip] = &rateBucket{count: 1, windowStart: now}
		return true
	}
	if b.count >= l.limit {
		return false
	}
	b.count++
	return true
}

// sweepLocked drops expired buckets at most once per window so the map
// can't grow without bound under a distinct-IP flood.
func (l *ipRateLimiter) sweepLocked(now time.Time) {
	if now.Sub(l.lastSwep) < l.window {
		return
	}
	l.lastSwep = now
	for ip, b := range l.buckets {
		if now.Sub(b.windowStart) >= l.window {
			delete(l.buckets, ip)
		}
	}
}

// rateLimitMiddleware rejects requests from an IP that exceeds the limiter
// with 429 before the handler (and any DB work) runs.
func rateLimitMiddleware(l *ipRateLimiter, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !l.allow(clientIP(r)) {
			w.Header().Set("Retry-After", "1")
			writeErr(w, http.StatusTooManyRequests, "rate_limited", "too many requests; slow down")
			return
		}
		next(w, r)
	}
}

// loginLimiter throttles password-login attempts per IP with a sliding
// failure count and an escalating lockout. Successful logins reset the
// counter. This raises the cost of online brute-forcing the catalog
// password well beyond the per-attempt bcrypt cost alone.
type loginLimiter struct {
	mu          sync.Mutex
	attempts    map[string]*loginAttempt
	maxFailures int
	lockout     time.Duration
	lastSwep    time.Time
}

type loginAttempt struct {
	failures    int
	lockedUntil time.Time
	lastSeen    time.Time
}

func newLoginLimiter(maxFailures int, lockout time.Duration) *loginLimiter {
	return &loginLimiter{
		attempts:    map[string]*loginAttempt{},
		maxFailures: maxFailures,
		lockout:     lockout,
		lastSwep:    time.Now(),
	}
}

// allow reports whether ip may attempt a login now, and the remaining
// lockout duration when it may not.
func (l *loginLimiter) allow(ip string) (bool, time.Duration) {
	if l == nil {
		return true, 0
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweepLocked(now)
	a, ok := l.attempts[ip]
	if !ok {
		return true, 0
	}
	if now.Before(a.lockedUntil) {
		return false, a.lockedUntil.Sub(now)
	}
	return true, 0
}

// recordFailure increments the failure count for ip and arms a lockout
// once the threshold is crossed.
func (l *loginLimiter) recordFailure(ip string) {
	if l == nil {
		return
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.attempts[ip]
	if !ok {
		a = &loginAttempt{}
		l.attempts[ip] = a
	}
	a.failures++
	a.lastSeen = now
	if a.failures >= l.maxFailures {
		a.lockedUntil = now.Add(l.lockout)
		// Reset the counter so each subsequent breach re-arms a fresh
		// lockout window rather than locking out permanently.
		a.failures = 0
	}
}

// recordSuccess clears any failure state for ip.
func (l *loginLimiter) recordSuccess(ip string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, ip)
}

func (l *loginLimiter) sweepLocked(now time.Time) {
	if now.Sub(l.lastSwep) < l.lockout {
		return
	}
	l.lastSwep = now
	for ip, a := range l.attempts {
		if now.After(a.lockedUntil) && now.Sub(a.lastSeen) > l.lockout {
			delete(l.attempts, ip)
		}
	}
}

// bcryptSemaphore caps the number of concurrent bcrypt verifications so a
// burst of login requests can't pin every CPU (bcrypt is deliberately
// expensive). Acquire blocks until a slot frees or the request context is
// cancelled.
type bcryptSemaphore struct {
	slots chan struct{}
}

func newBcryptSemaphore(n int) *bcryptSemaphore {
	if n < 1 {
		n = 1
	}
	return &bcryptSemaphore{slots: make(chan struct{}, n)}
}

// acquire blocks for a verification slot. Returns false if the supplied
// done channel (the request context) fires first.
func (s *bcryptSemaphore) acquire(done <-chan struct{}) bool {
	if s == nil {
		return true
	}
	select {
	case s.slots <- struct{}{}:
		return true
	case <-done:
		return false
	}
}

func (s *bcryptSemaphore) release() {
	if s == nil {
		return
	}
	select {
	case <-s.slots:
	default:
	}
}
