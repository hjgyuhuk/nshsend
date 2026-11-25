package receive

import (
	"net/http"
	"sync"
)

const maxPinAttempts = 3

// PinAttemptTracker enforces per-IP PIN attempt limits matching LocalSend (max 3 failures -> 429).
type PinAttemptTracker struct {
	mu       sync.Mutex
	attempts map[string]int
}

// NewPinAttemptTracker creates a PIN attempt tracker.
func NewPinAttemptTracker() *PinAttemptTracker {
	return &PinAttemptTracker{
		attempts: make(map[string]int),
	}
}

// ValidatePin checks provided against required for remoteIP.
// Returns (httpStatus, errMessage) when rejected, or (0, "") when allowed.
func (p *PinAttemptTracker) ValidatePin(required string, provided string, hasParam bool, remoteIP string) (int, string) {
	if required == "" {
		return 0, ""
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.attempts[remoteIP] >= maxPinAttempts {
		return http.StatusTooManyRequests, "Too many requests"
	}
	if !hasParam {
		return http.StatusUnauthorized, "PIN required"
	}
	if provided == required {
		delete(p.attempts, remoteIP)
		return 0, ""
	}
	if len(p.attempts) > 256 {
		clear(p.attempts)
	}
	p.attempts[remoteIP]++
	return http.StatusUnauthorized, "Invalid PIN"
}
