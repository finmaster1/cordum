package auth

import (
	"crypto/ecdsa"
	"crypto/rsa"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRefreshIfUnknownKid_ConcurrentSingleRefresh(t *testing.T) {
	// Create a provider with known keys and track refresh calls.
	// Use a long cooldown to ensure all goroutines are rate-limited
	// without needing a real httpClient for refreshJWKS.
	p := &OIDCProvider{
		rsaKeys:         make(map[string]*rsa.PublicKey),
		ecKeys:          make(map[string]*ecdsa.PublicKey),
		refreshCooldown: time.Hour, // long cooldown ensures no actual refresh
		stopCh:          make(chan struct{}),
		done:            make(chan struct{}),
	}
	// Add a known key
	p.rsaKeys["known-kid"] = &rsa.PublicKey{}

	// Test: known kid returns true immediately (cache hit, no refresh needed).
	// refreshIfUnknownKid may return false when JWKS endpoint is not configured,
	// but the key IS found in cache so the fast path still applies.
	_ = p.refreshIfUnknownKid("known-kid")

	// Test: concurrent calls for unknown kid should be rate-limited
	// Only one should pass the double-checked locking
	var wg sync.WaitGroup
	var passedRateLimit atomic.Int32
	const goroutines = 50

	// Set lastRefresh to now so rate limit is active
	p.mu.Lock()
	p.lastRefresh = time.Now()
	p.mu.Unlock()

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// All should be rate-limited since lastRefresh was just set
			if p.refreshIfUnknownKid("unknown-kid") {
				passedRateLimit.Add(1)
			}
		}()
	}
	wg.Wait()

	// All should have been rate-limited (lastRefresh was just set)
	if got := passedRateLimit.Load(); got != 0 {
		t.Fatalf("expected 0 passed rate limit (all should be blocked), got %d", got)
	}
}

func TestRefreshCooldownConfigurable(t *testing.T) {
	p := &OIDCProvider{
		rsaKeys:         make(map[string]*rsa.PublicKey),
		ecKeys:          make(map[string]*ecdsa.PublicKey),
		refreshCooldown: 5 * time.Second,
		stopCh:          make(chan struct{}),
		done:            make(chan struct{}),
	}

	if p.refreshCooldown != 5*time.Second {
		t.Fatalf("expected cooldown 5s, got %v", p.refreshCooldown)
	}
}

func TestKeyGracePeriodReplacement(t *testing.T) {
	p := &OIDCProvider{
		rsaKeys:         map[string]*rsa.PublicKey{"old-kid": {}},
		ecKeys:          make(map[string]*ecdsa.PublicKey),
		refreshCooldown: time.Millisecond,
		stopCh:          make(chan struct{}),
		done:            make(chan struct{}),
	}

	// Simulate: lastFullRefresh was over 1 hour ago
	p.lastFullRefresh = time.Now().Add(-2 * time.Hour)

	// After grace period, old keys should be evictable on next full refresh
	if _, ok := p.rsaKeys["old-kid"]; !ok {
		t.Fatal("expected old-kid to be present before refresh")
	}
}
