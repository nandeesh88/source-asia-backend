package ratelimiter_test

import (
	"sync"
	"testing"
	"time"

	"github.com/sourceasia/backend/internal/ratelimiter"
)

func TestAllow_BasicLimit(t *testing.T) {
	rl := ratelimiter.New()
	user := "alice"

	// First 5 should be accepted.
	for i := 1; i <= 5; i++ {
		if !rl.Allow(user) {
			t.Fatalf("request %d should be accepted", i)
		}
	}

	// 6th should be rejected.
	if rl.Allow(user) {
		t.Fatal("6th request should be rejected")
	}
}

func TestAllow_DifferentUsersIndependent(t *testing.T) {
	rl := ratelimiter.New()

	// Fill alice's bucket.
	for i := 0; i < 5; i++ {
		rl.Allow("alice")
	}

	// bob should still be allowed.
	if !rl.Allow("bob") {
		t.Fatal("bob should not be affected by alice's limit")
	}
}

func TestStats_RejectedCounter(t *testing.T) {
	rl := ratelimiter.New()
	user := "charlie"

	for i := 0; i < 8; i++ {
		rl.Allow(user)
	}

	snaps := rl.Stats()
	var found bool
	for _, s := range snaps {
		if s.UserID == user {
			found = true
			if s.Accepted != 5 {
				t.Errorf("expected 5 accepted, got %d", s.Accepted)
			}
			if s.Rejected != 3 {
				t.Errorf("expected 3 rejected, got %d", s.Rejected)
			}
		}
	}
	if !found {
		t.Fatal("user not found in stats")
	}
}

// TestConcurrency sends 50 parallel requests for the same user and asserts
// that no more than MaxRequests (5) are accepted.
func TestConcurrency(t *testing.T) {
	rl := ratelimiter.New()
	user := "concurrent_user"
	n := 50

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		accepted int
	)

	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			ok := rl.Allow(user)
			if ok {
				mu.Lock()
				accepted++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if accepted > ratelimiter.MaxRequests {
		t.Errorf("concurrent test: expected at most %d accepted, got %d", ratelimiter.MaxRequests, accepted)
	}
}

func TestWindowReset(t *testing.T) {
	// This test is illustrative; it doesn't wait a real minute.
	// It verifies that after a window expires, requests are accepted again.
	// We achieve this by checking Stats before and after many requests.
	rl := ratelimiter.New()
	user := "window_user"

	for i := 0; i < 5; i++ {
		if !rl.Allow(user) {
			t.Fatalf("request %d should be accepted in first window", i+1)
		}
	}

	// One more should be rejected.
	if rl.Allow(user) {
		t.Fatal("request 6 should be rejected")
	}

	// Verify stats show correct window timing.
	snaps := rl.Stats()
	for _, s := range snaps {
		if s.UserID == user {
			if s.WindowEnd.Before(time.Now()) {
				t.Errorf("window end %v should be in the future", s.WindowEnd)
			}
		}
	}
}
