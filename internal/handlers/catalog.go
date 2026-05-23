package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sourceasia/backend/internal/catalog"
	"github.com/sourceasia/backend/internal/models"
)

const (
	defaultLimit = 20
	maxLimit     = 100
)

// CatalogHandler handles all /products endpoints.
type CatalogHandler struct {
	store *catalog.Store
}

// NewCatalogHandler creates a handler backed by the given store.
func NewCatalogHandler(s *catalog.Store) *CatalogHandler {
	return &CatalogHandler{store: s}
}

// HandleProducts routes between POST /products and GET /products.
func (h *CatalogHandler) HandleProducts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.createProduct(w, r)
	case http.MethodGet:
		h.listProducts(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
	}
}

// HandleProductByID routes requests to /products/{id}.
func (h *CatalogHandler) HandleProductByID(w http.ResponseWriter, r *http.Request) {
	// Extract id from the path: /products/{id} or /products/{id}/media
	path := strings.TrimPrefix(r.URL.Path, "/products/")
	parts := strings.SplitN(path, "/", 2)
	id := parts[0]

	if id == "" {
		writeError(w, http.StatusBadRequest, "product id is required", "")
		return
	}

	// /products/{id}/media
	if len(parts) == 2 && parts[1] == "media" {
		if r.Method == http.MethodPost {
			h.addMedia(w, r, id)
		} else {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		}
		return
	}

	// /products/{id}
	if r.Method == http.MethodGet {
		h.getProduct(w, r, id)
	} else {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
	}
}

// ─── createProduct: POST /products ───────────────────────────────────────────

func (h *CatalogHandler) createProduct(w http.ResponseWriter, r *http.Request) {
	var req models.CreateProductRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body", err.Error())
		return
	}

	// Nil-safe: treat null JSON arrays as empty slices.
	if req.ImageURLs == nil {
		req.ImageURLs = []string{}
	}
	if req.VideoURLs == nil {
		req.VideoURLs = []string{}
	}

	p, err := h.store.Create(req)
	if err != nil {
		if err == catalog.ErrDuplicateSKU {
			writeError(w, http.StatusConflict, "duplicate SKU", "a product with this SKU already exists; SKUs must be unique")
			return
		}
		writeError(w, http.StatusBadRequest, "validation error", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, toDetail(p))
}

// ─── listProducts: GET /products ─────────────────────────────────────────────

func (h *CatalogHandler) listProducts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	limit := defaultLimit
	if ls := q.Get("limit"); ls != "" {
		v, err := strconv.Atoi(ls)
		if err != nil || v < 1 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer", "")
			return
		}
		if v > maxLimit {
			v = maxLimit
		}
		limit = v
	}

	offset := 0
	if os := q.Get("offset"); os != "" {
		v, err := strconv.Atoi(os)
		if err != nil || v < 0 {
			writeError(w, http.StatusBadRequest, "offset must be a non-negative integer", "")
			return
		}
		offset = v
	}

	result := h.store.List(limit, offset)
	writeJSON(w, http.StatusOK, result)
}

// ─── getProduct: GET /products/{id} ──────────────────────────────────────────

func (h *CatalogHandler) getProduct(w http.ResponseWriter, r *http.Request, id string) {
	p, err := h.store.Get(id)
	if err != nil {
		if err == catalog.ErrNotFound {
			writeError(w, http.StatusNotFound, "product not found", "")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toDetail(p))
}

// ─── addMedia: POST /products/{id}/media ─────────────────────────────────────

func (h *CatalogHandler) addMedia(w http.ResponseWriter, r *http.Request, id string) {
	var req models.AddMediaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body", err.Error())
		return
	}

	if req.ImageURLs == nil {
		req.ImageURLs = []string{}
	}
	if req.VideoURLs == nil {
		req.VideoURLs = []string{}
	}

	p, err := h.store.AddMedia(id, req)
	if err != nil {
		if err == catalog.ErrNotFound {
			writeError(w, http.StatusNotFound, "product not found", "")
			return
		}
		// validation errors or empty body
		writeError(w, http.StatusBadRequest, "validation error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, toDetail(p))
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func toDetail(p *models.Product) models.ProductDetail {
	imgs := p.ImageURLs
	if imgs == nil {
		imgs = []string{}
	}
	vids := p.VideoURLs
	if vids == nil {
		vids = []string{}
	}
	return models.ProductDetail{
		ID:        p.ID,
		Name:      p.Name,
		SKU:       p.SKU,
		ImageURLs: imgs,
		VideoURLs: vids,
		CreatedAt: p.CreatedAt.Format(time.RFC3339),
	}
}
