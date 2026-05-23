package models

import "time"

// ─── Part 1: Rate Limiter ─────────────────────────────────────────────────────

// RequestBody is the body expected by POST /request.
type RequestBody struct {
	UserID  string      `json:"user_id"`
	Payload interface{} `json:"payload"`
}

// RequestResponse is returned on a successful POST /request (201 Created).
type RequestResponse struct {
	Status    string `json:"status"`
	UserID    string `json:"user_id"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

// UserStats holds per-user rate-limit statistics.
type UserStats struct {
	UserID           string `json:"user_id"`
	AcceptedInWindow int    `json:"accepted_in_window"`   // accepted in current 1-min window
	RejectedTotal    int    `json:"rejected_total"`        // cumulative rejections (all time)
	WindowStartsAt   string `json:"window_starts_at"`     // when the current window opened
	WindowEndsAt     string `json:"window_ends_at"`       // when the current window closes
}

// StatsResponse is returned by GET /stats.
type StatsResponse struct {
	Users     []UserStats `json:"users"`
	UpdatedAt string      `json:"updated_at"`
}

// ─── Part 2: Product Catalog ──────────────────────────────────────────────────

// Product is the full internal representation stored in memory.
type Product struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	SKU       string    `json:"sku"`
	ImageURLs []string  `json:"image_urls"`
	VideoURLs []string  `json:"video_urls"`
	CreatedAt time.Time `json:"created_at"`
}

// ProductListItem is the lightweight object returned by GET /products.
// Full media arrays are intentionally omitted for performance.
type ProductListItem struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	SKU          string `json:"sku"`
	ImageCount   int    `json:"image_count"`
	VideoCount   int    `json:"video_count"`
	ThumbnailURL string `json:"thumbnail_url,omitempty"` // first image URL if any
	CreatedAt    string `json:"created_at"`
}

// ProductDetail is the full object returned by GET /products/{id}.
type ProductDetail struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	SKU       string   `json:"sku"`
	ImageURLs []string `json:"image_urls"`
	VideoURLs []string `json:"video_urls"`
	CreatedAt string   `json:"created_at"`
}

// ProductListResponse wraps the paginated list result.
type ProductListResponse struct {
	Products []ProductListItem `json:"products"`
	Total    int               `json:"total"`
	Limit    int               `json:"limit"`
	Offset   int               `json:"offset"`
}

// CreateProductRequest is the body for POST /products.
type CreateProductRequest struct {
	Name      string   `json:"name"`
	SKU       string   `json:"sku"`
	ImageURLs []string `json:"image_urls"`
	VideoURLs []string `json:"video_urls"`
}

// AddMediaRequest is the body for POST /products/{id}/media.
type AddMediaRequest struct {
	ImageURLs []string `json:"image_urls"`
	VideoURLs []string `json:"video_urls"`
}

// ErrorResponse is a generic JSON error envelope.
type ErrorResponse struct {
	Error   string `json:"error"`
	Details string `json:"details,omitempty"`
}
