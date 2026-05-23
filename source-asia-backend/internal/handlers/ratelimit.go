package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/sourceasia/backend/internal/models"
	"github.com/sourceasia/backend/internal/ratelimiter"
)

// RateLimitHandler handles POST /request and GET /stats.
type RateLimitHandler struct {
	limiter *ratelimiter.RateLimiter
}

// NewRateLimitHandler creates a handler backed by the given limiter.
func NewRateLimitHandler(rl *ratelimiter.RateLimiter) *RateLimitHandler {
	return &RateLimitHandler{limiter: rl}
}

// HandleRequest handles POST /request.
func (h *RateLimitHandler) HandleRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	// Decode body.
	var body models.RequestBody
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body", err.Error())
		return
	}

	// Validate user_id.
	if strings.TrimSpace(body.UserID) == "" {
		writeError(w, http.StatusBadRequest, "user_id is required and must not be empty", "")
		return
	}

	// Validate payload is present (non-nil).
	if body.Payload == nil {
		writeError(w, http.StatusBadRequest, "payload is required", "")
		return
	}

	// Check rate limit.
	if !h.limiter.Allow(body.UserID) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(models.ErrorResponse{
			Error:   "rate limit exceeded",
			Details: "maximum 5 requests per minute per user; please wait before retrying",
		})
		return
	}

	resp := models.RequestResponse{
		Status:    "accepted",
		UserID:    body.UserID,
		Message:   "request has been accepted and will be processed",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	writeJSON(w, http.StatusCreated, resp)
}

// HandleStats handles GET /stats.
func (h *RateLimitHandler) HandleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	snaps := h.limiter.Stats()

	userStats := make([]models.UserStats, 0, len(snaps))
	for _, s := range snaps {
		userStats = append(userStats, models.UserStats{
			UserID:           s.UserID,
			AcceptedInWindow: s.Accepted,
			RejectedTotal:    s.Rejected,
			WindowStartsAt:   s.WindowStart.Format(time.RFC3339),
			WindowEndsAt:     s.WindowEnd.Format(time.RFC3339),
		})
	}

	resp := models.StatsResponse{
		Users:     userStats,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	writeJSON(w, http.StatusOK, resp)
}
