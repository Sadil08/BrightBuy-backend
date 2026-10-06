// Package ratelimit is the shared in-memory token-bucket limiter specs/global/02_SECURITY_BASELINE.md
// §4 calls for on login/registration/checkout: "slow credential stuffing and cart/checkout abuse."
// In-memory, per-instance, deliberately (plan.md §5.3, specs/global/04_SCALABILITY.md §1): Phase 1 is
// a single backend instance, so there's no consistency problem a shared store would be solving yet —
// move to Redis or similar only once there's more than one instance, since a per-instance bucket
// stops being effective at that point (each instance would allow its own separate quota).
package ratelimit

import (
	"net"
	"net/http"
	"sync"

	"golang.org/x/time/rate"

	"brightbuy-backend/internal/shared/httpx"
)

// Limiter rate-limits an arbitrary string key (an IP, an email, anything), each key getting its own
// independent token bucket. One Limiter = one policy (e.g. "5 per minute") shared across however many
// distinct keys show up — a new key just gets a fresh bucket the first time it's seen.
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*rate.Limiter
	// Known, accepted limitation: buckets never shrinks — a key seen once keeps a tiny entry
	// forever. At Phase 1 scale (login/register traffic, not millions of unique IPs) this is
	// negligible; worth a periodic sweep only if this ever actually shows up as a real memory
	// concern, not something to build pre-emptively against a problem that may never arrive.
	r     rate.Limit
	burst int
}

// New returns a Limiter allowing `burst` requests immediately per key, refilling at `eventsPerSecond`
// thereafter — e.g. New(5.0/60.0, 5) is "5 per minute, with an initial burst of up to 5 at once."
func New(eventsPerSecond float64, burst int) *Limiter {
	return &Limiter{
		buckets: make(map[string]*rate.Limiter),
		r:       rate.Limit(eventsPerSecond),
		burst:   burst,
	}
}

// Allow reports whether a request keyed by key is within the limit right now, consuming one token
// from that key's bucket if so. Safe for concurrent use.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	bucket, ok := l.buckets[key]
	if !ok {
		bucket = rate.NewLimiter(l.r, l.burst)
		l.buckets[key] = bucket
	}
	return bucket.Allow()
}

// Middleware wraps a handler, rejecting with 429 any request whose key (computed by keyFunc) has
// exceeded the limit. Use this for a key available before the handler runs (an IP, a path param); a
// key that only exists in a parsed request BODY (like a login attempt's submitted email) has to be
// checked with a direct Allow(...) call inside the handler instead — see identity/httpapi's
// AuthHandler.Login for that case.
func (l *Limiter) Middleware(keyFunc func(r *http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !l.Allow(keyFunc(r)) {
				httpx.WriteError(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many requests, try again later")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ByIP extracts the real TCP peer address from r.RemoteAddr — NOT a client-supplied
// X-Forwarded-For/X-Real-IP header, which is spoofable and would make this limiter trivially
// bypassable (specs/global/02_SECURITY_BASELINE.md §4, the same reasoning cmd/api/main.go's comment
// gives for not using chi's middleware.RealIP without a trusted reverse proxy in front).
// r.RemoteAddr is "host:port"; SplitHostPort strips the port since two requests from the same IP on
// different ephemeral ports must share one bucket, not get one each.
func ByIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
