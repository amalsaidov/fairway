package api

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type clientBucket struct {
	tokens     float64
	lastRefill time.Time
	lastSeen   time.Time
}

// RateLimiter implements an in-memory per-client token bucket rate limiter
// using only the Go standard library.
type RateLimiter struct {
	mu         sync.Mutex
	clients    map[string]*clientBucket
	rps        float64
	burst      float64
	trustProxy bool
	stopOnce   sync.Once
	stopCh     chan struct{}
}

// NewRateLimiter initializes a per-client token bucket rate limiter.
func NewRateLimiter(rps float64, burst int, trustProxy bool) *RateLimiter {
	if rps <= 0 {
		rps = 10.0
	}
	if burst <= 0 {
		burst = 30
	}
	rl := &RateLimiter{
		clients:    make(map[string]*clientBucket),
		rps:        rps,
		burst:      float64(burst),
		trustProxy: trustProxy,
		stopCh:     make(chan struct{}),
	}
	go rl.cleanupLoop()
	return rl
}

// Stop shuts down the background eviction goroutine.
func (rl *RateLimiter) Stop() {
	rl.stopOnce.Do(func() {
		close(rl.stopCh)
	})
}

func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-rl.stopCh:
			return
		case now := <-ticker.C:
			rl.cleanup(now)
		}
	}
}

func (rl *RateLimiter) cleanup(now time.Time) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	for ip, b := range rl.clients {
		if now.Sub(b.lastSeen) > 3*time.Minute {
			delete(rl.clients, ip)
		}
	}
}

// ClientIP extracts the client IP address from the HTTP request.
// When trustProxy is false (default), it reads directly from RemoteAddr.
// When trustProxy is true, it inspects the X-Forwarded-For header first.
func (rl *RateLimiter) ClientIP(r *http.Request) string {
	if rl.trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if len(parts) > 0 {
				ip := strings.TrimSpace(parts[0])
				if ip != "" {
					return ip
				}
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// Allow evaluates whether a request from the specified IP is within limits.
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, exists := rl.clients[ip]
	if !exists {
		rl.clients[ip] = &clientBucket{
			tokens:     rl.burst - 1.0,
			lastRefill: now,
			lastSeen:   now,
		}
		return true
	}

	b.lastSeen = now

	// Refill tokens according to elapsed duration
	elapsed := now.Sub(b.lastRefill).Seconds()
	b.lastRefill = now
	b.tokens += elapsed * rl.rps
	if b.tokens > rl.burst {
		b.tokens = rl.burst
	}

	if b.tokens >= 1.0 {
		b.tokens -= 1.0
		return true
	}

	return false
}
