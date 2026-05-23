// Package ratelimiter implements a fixed 1-minute window rate limiter.
//
// Design: each user gets a window that is pinned to the clock minute in which
// their first request in that window arrived.  When the current time is past
// windowStart + 60 s, the counter resets and a new window begins.
//
// Concurrency: a single sync.RWMutex guards the entire user map. Writers
// (POST /request) acquire a full write-lock so no two goroutines can race on
// the same user's counter.
package ratelimiter

import (
	"sync"
	"time"
)

const (
	// MaxRequests is the number of accepted requests allowed per user per window.
	MaxRequests = 5
	// WindowDuration is the length of each rate-limit window.
	WindowDuration = time.Minute
)

// userWindow holds the state for a single user inside one time window.
type userWindow struct {
	windowStart time.Time
	accepted    int
	rejected    int // cumulative across all windows
}

// RateLimiter is safe for concurrent use.
type RateLimiter struct {
	mu    sync.RWMutex
	users map[string]*userWindow
}

// New creates an initialised RateLimiter.
func New() *RateLimiter {
	return &RateLimiter{
		users: make(map[string]*userWindow),
	}
}

// Allow checks whether userID may make another request right now.
// It increments the appropriate counter and returns true when the request
// is accepted, false when it is rate-limited.
func (rl *RateLimiter) Allow(userID string) bool {
	now := time.Now()

	rl.mu.Lock()
	defer rl.mu.Unlock()

	w, exists := rl.users[userID]
	if !exists {
		// First-ever request from this user.
		rl.users[userID] = &userWindow{
			windowStart: now,
			accepted:    1,
			rejected:    0,
		}
		return true
	}

	// Check whether the current window has expired.
	if now.Sub(w.windowStart) >= WindowDuration {
		// Start a new window; carry forward cumulative rejected count.
		w.windowStart = now
		w.accepted = 1
		return true
	}

	// Still inside the same window.
	if w.accepted < MaxRequests {
		w.accepted++
		return true
	}

	// Limit exceeded.
	w.rejected++
	return false
}

// Stats returns a snapshot of all users' counters.
// Callers receive value copies so the internal state cannot be mutated.
type Snapshot struct {
	UserID         string
	Accepted       int
	Rejected       int
	WindowStart    time.Time
	WindowEnd      time.Time
}

func (rl *RateLimiter) Stats() []Snapshot {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	snaps := make([]Snapshot, 0, len(rl.users))
	for id, w := range rl.users {
		snaps = append(snaps, Snapshot{
			UserID:      id,
			Accepted:    w.accepted,
			Rejected:    w.rejected,
			WindowStart: w.windowStart,
			WindowEnd:   w.windowStart.Add(WindowDuration),
		})
	}
	return snaps
}
