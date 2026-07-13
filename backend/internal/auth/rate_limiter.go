package auth

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"hopper/backend/internal/response"
)

type RateLimiter struct {
	limit    int
	mu       sync.Mutex
	attempts map[string]rateState
}

type rateState struct {
	windowStart time.Time
	count       int
}

func NewRateLimiter(limit int) *RateLimiter {
	return &RateLimiter{
		limit:    limit,
		attempts: make(map[string]rateState),
	}
}

func (r *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !r.allow(clientIP(req)) {
			response.WriteError(w, http.StatusTooManyRequests, "rate_limited", "too many authentication requests")
			return
		}
		next.ServeHTTP(w, req)
	})
}

func (r *RateLimiter) allow(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	if len(r.attempts) >= 10000 {
		for candidate, candidateState := range r.attempts {
			if now.Sub(candidateState.windowStart) >= time.Minute {
				delete(r.attempts, candidate)
			}
		}
		if _, exists := r.attempts[key]; !exists && len(r.attempts) >= 10000 {
			return false
		}
	}
	state := r.attempts[key]
	if state.windowStart.IsZero() || now.Sub(state.windowStart) >= time.Minute {
		r.attempts[key] = rateState{windowStart: now, count: 1}
		return true
	}

	if state.count >= r.limit {
		return false
	}

	state.count++
	r.attempts[key] = state
	return true
}

func clientIP(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get("X-Real-IP")); forwarded != "" && net.ParseIP(forwarded) != nil {
		return forwarded
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
