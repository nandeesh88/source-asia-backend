// Package catalog provides an in-memory product store.
//
// Data model
// ──────────
//   products  map[id] → Product        (full data including all URL slices)
//   skuIndex  map[sku] → id            (fast duplicate-SKU detection)
//   order     []string                 (insertion-order slice of IDs for stable pagination)
//
// List vs detail
// ──────────────
// GET /products builds each ProductListItem directly from the Product struct,
// copying only scalar fields (ID, Name, SKU, CreatedAt) and the *lengths* of
// the URL slices — it never allocates or serialises the URL strings themselves.
// The slice lengths are O(1) reads; no iteration over media URLs occurs.
//
// GET /products/{id} returns the full Product including all URL slices.
//
// With PostgreSQL in production you would store products and media in separate
// tables; the list query would SELECT id, name, sku, created_at,
// COUNT(image_id), COUNT(video_id) — never fetching URL strings — while the
// detail query would JOIN or use a second query to fetch media rows.
package catalog

import (
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/sourceasia/backend/internal/models"
)

const (
	MaxURLsPerRequest = 20   // maximum image_urls or video_urls in a single request
	MaxURLLength      = 2048 // maximum characters for a single URL string
)

// Store is safe for concurrent use.
type Store struct {
	mu       sync.RWMutex
	products map[string]*models.Product // id → product
	skuIndex map[string]string          // sku → id
	order    []string                   // insertion order of ids
	counter  int                        // monotonic id counter
}

// New creates an empty, initialised Store.
func New() *Store {
	return &Store{
		products: make(map[string]*models.Product),
		skuIndex: make(map[string]string),
	}
}

// ─── Validation helpers ───────────────────────────────────────────────────────

// ValidateURL returns an error if s is not a valid http/https URL within the
// maximum allowed length.
func ValidateURL(s string) error {
	if len(s) == 0 {
		return fmt.Errorf("URL must not be empty")
	}
	if len(s) > MaxURLLength {
		return fmt.Errorf("URL exceeds maximum length of %d characters", MaxURLLength)
	}
	u, err := url.ParseRequestURI(s)
	if err != nil {
		return fmt.Errorf("URL %q is not valid: %v", s, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("URL %q must use http or https scheme", s)
	}
	if u.Host == "" {
		return fmt.Errorf("URL %q must have a host", s)
	}
	return nil
}

// validateURLSlice validates a slice of URLs and enforces the per-request cap.
func validateURLSlice(urls []string, field string) error {
	if len(urls) > MaxURLsPerRequest {
		return fmt.Errorf("%s: maximum %d URLs per request, got %d", field, MaxURLsPerRequest, len(urls))
	}
	for _, u := range urls {
		if err := ValidateURL(u); err != nil {
			return fmt.Errorf("%s: %v", field, err)
		}
	}
	return nil
}

// ─── CRUD operations ──────────────────────────────────────────────────────────

// ErrDuplicateSKU is returned when a product with the given SKU already exists.
var ErrDuplicateSKU = fmt.Errorf("a product with this SKU already exists")

// ErrNotFound is returned when a product id does not exist.
var ErrNotFound = fmt.Errorf("product not found")

// Create validates and inserts a new product. Returns the created product or an
// error.
func (s *Store) Create(req models.CreateProductRequest) (*models.Product, error) {
	// Validate required fields.
	if strings.TrimSpace(req.Name) == "" {
		return nil, fmt.Errorf("name is required and must not be empty")
	}
	if strings.TrimSpace(req.SKU) == "" {
		return nil, fmt.Errorf("sku is required and must not be empty")
	}
	if err := validateURLSlice(req.ImageURLs, "image_urls"); err != nil {
		return nil, err
	}
	if err := validateURLSlice(req.VideoURLs, "video_urls"); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Duplicate SKU check.
	if _, exists := s.skuIndex[req.SKU]; exists {
		return nil, ErrDuplicateSKU
	}

	s.counter++
	id := fmt.Sprintf("prod_%06d", s.counter)

	// Defensive copies of URL slices.
	imgURLs := make([]string, len(req.ImageURLs))
	copy(imgURLs, req.ImageURLs)
	vidURLs := make([]string, len(req.VideoURLs))
	copy(vidURLs, req.VideoURLs)

	p := &models.Product{
		ID:        id,
		Name:      strings.TrimSpace(req.Name),
		SKU:       strings.TrimSpace(req.SKU),
		ImageURLs: imgURLs,
		VideoURLs: vidURLs,
		CreatedAt: time.Now().UTC(),
	}

	s.products[id] = p
	s.skuIndex[p.SKU] = id
	s.order = append(s.order, id)

	return p, nil
}

// List returns a paginated slice of lightweight list items.
// Media URL strings are never loaded or serialised — only counts are copied.
func (s *Store) List(limit, offset int) models.ProductListResponse {
	s.mu.RLock()
	defer s.mu.RUnlock()

	total := len(s.order)

	if offset >= total {
		return models.ProductListResponse{
			Products: []models.ProductListItem{},
			Total:    total,
			Limit:    limit,
			Offset:   offset,
		}
	}

	end := offset + limit
	if end > total {
		end = total
	}
	page := s.order[offset:end]

	items := make([]models.ProductListItem, 0, len(page))
	for _, id := range page {
		p := s.products[id]

		var thumb string
		if len(p.ImageURLs) > 0 {
			thumb = p.ImageURLs[0]
		}

		items = append(items, models.ProductListItem{
			ID:           p.ID,
			Name:         p.Name,
			SKU:          p.SKU,
			ImageCount:   len(p.ImageURLs),
			VideoCount:   len(p.VideoURLs),
			ThumbnailURL: thumb,
			CreatedAt:    p.CreatedAt.Format(time.RFC3339),
		})
	}

	return models.ProductListResponse{
		Products: items,
		Total:    total,
		Limit:    limit,
		Offset:   offset,
	}
}

// Get returns the full product detail for id.
func (s *Store) Get(id string) (*models.Product, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	p, ok := s.products[id]
	if !ok {
		return nil, ErrNotFound
	}
	// Return a deep copy so callers cannot mutate store internals.
	cp := *p
	cp.ImageURLs = append([]string(nil), p.ImageURLs...)
	cp.VideoURLs = append([]string(nil), p.VideoURLs...)
	return &cp, nil
}

// AddMedia appends new URLs to an existing product.
func (s *Store) AddMedia(id string, req models.AddMediaRequest) (*models.Product, error) {
	// Validate before acquiring the write lock.
	if len(req.ImageURLs) == 0 && len(req.VideoURLs) == 0 {
		return nil, fmt.Errorf("at least one of image_urls or video_urls must be provided")
	}
	if err := validateURLSlice(req.ImageURLs, "image_urls"); err != nil {
		return nil, err
	}
	if err := validateURLSlice(req.VideoURLs, "video_urls"); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	p, ok := s.products[id]
	if !ok {
		return nil, ErrNotFound
	}

	p.ImageURLs = append(p.ImageURLs, req.ImageURLs...)
	p.VideoURLs = append(p.VideoURLs, req.VideoURLs...)

	cp := *p
	cp.ImageURLs = append([]string(nil), p.ImageURLs...)
	cp.VideoURLs = append([]string(nil), p.VideoURLs...)
	return &cp, nil
}

// Count returns the total number of products (used for health checks / tests).
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.order)
}
