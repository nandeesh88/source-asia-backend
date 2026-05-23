package catalog_test

import (
	"fmt"
	"testing"

	"github.com/sourceasia/backend/internal/catalog"
	"github.com/sourceasia/backend/internal/models"
)

func newStore() *catalog.Store { return catalog.New() }

func validProduct(n int) models.CreateProductRequest {
	return models.CreateProductRequest{
		Name:      fmt.Sprintf("Product %d", n),
		SKU:       fmt.Sprintf("SKU-%04d", n),
		ImageURLs: []string{"https://cdn.example.com/img-1.jpg"},
		VideoURLs: []string{"https://cdn.example.com/vid-1.mp4"},
	}
}

func TestCreate_Success(t *testing.T) {
	s := newStore()
	p, err := s.Create(validProduct(1))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.ID == "" {
		t.Error("expected non-empty ID")
	}
	if p.Name != "Product 1" {
		t.Errorf("unexpected name %q", p.Name)
	}
}

func TestCreate_DuplicateSKU(t *testing.T) {
	s := newStore()
	req := validProduct(1)
	s.Create(req)
	_, err := s.Create(req)
	if err != catalog.ErrDuplicateSKU {
		t.Errorf("expected ErrDuplicateSKU, got %v", err)
	}
}

func TestCreate_EmptyName(t *testing.T) {
	s := newStore()
	req := validProduct(1)
	req.Name = "   "
	_, err := s.Create(req)
	if err == nil {
		t.Error("expected error for empty name")
	}
}

func TestCreate_InvalidURL(t *testing.T) {
	s := newStore()
	req := validProduct(1)
	req.ImageURLs = []string{"not-a-url"}
	_, err := s.Create(req)
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

func TestCreate_TooManyURLs(t *testing.T) {
	s := newStore()
	req := validProduct(1)
	urls := make([]string, 21)
	for i := range urls {
		urls[i] = fmt.Sprintf("https://cdn.example.com/img-%d.jpg", i)
	}
	req.ImageURLs = urls
	_, err := s.Create(req)
	if err == nil {
		t.Error("expected error for too many URLs")
	}
}

func TestGet_NotFound(t *testing.T) {
	s := newStore()
	_, err := s.Get("does-not-exist")
	if err != catalog.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestList_Pagination(t *testing.T) {
	s := newStore()
	for i := 1; i <= 10; i++ {
		s.Create(validProduct(i))
	}

	r := s.List(3, 0)
	if len(r.Products) != 3 {
		t.Errorf("expected 3 items, got %d", len(r.Products))
	}
	if r.Total != 10 {
		t.Errorf("expected total 10, got %d", r.Total)
	}

	r2 := s.List(3, 9)
	if len(r2.Products) != 1 {
		t.Errorf("expected 1 item at tail, got %d", len(r2.Products))
	}

	r3 := s.List(5, 20)
	if len(r3.Products) != 0 {
		t.Error("expected 0 items when offset beyond end")
	}
}

func TestList_NoMediaURLs(t *testing.T) {
	s := newStore()
	req := validProduct(1)
	req.ImageURLs = []string{
		"https://cdn.example.com/a.jpg",
		"https://cdn.example.com/b.jpg",
	}
	s.Create(req)

	result := s.List(10, 0)
	if len(result.Products) != 1 {
		t.Fatal("expected 1 product")
	}
	item := result.Products[0]
	if item.ImageCount != 2 {
		t.Errorf("expected ImageCount 2, got %d", item.ImageCount)
	}
	// The list item is models.ProductListItem which has no ImageURLs field –
	// this is enforced at the type level, so no runtime assertion needed.
}

func TestAddMedia_AppendsURLs(t *testing.T) {
	s := newStore()
	p, _ := s.Create(validProduct(1))

	updated, err := s.AddMedia(p.ID, models.AddMediaRequest{
		ImageURLs: []string{"https://cdn.example.com/new.jpg"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(updated.ImageURLs) != 2 { // 1 original + 1 new
		t.Errorf("expected 2 image URLs, got %d", len(updated.ImageURLs))
	}
}

func TestAddMedia_EmptyBody(t *testing.T) {
	s := newStore()
	p, _ := s.Create(validProduct(1))
	_, err := s.AddMedia(p.ID, models.AddMediaRequest{})
	if err == nil {
		t.Error("expected error for empty media body")
	}
}

func TestAddMedia_NotFound(t *testing.T) {
	s := newStore()
	_, err := s.AddMedia("ghost", models.AddMediaRequest{
		ImageURLs: []string{"https://cdn.example.com/x.jpg"},
	})
	if err != catalog.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}
