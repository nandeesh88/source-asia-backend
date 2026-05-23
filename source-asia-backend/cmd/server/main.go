package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/sourceasia/backend/internal/catalog"
	"github.com/sourceasia/backend/internal/handlers"
	"github.com/sourceasia/backend/internal/ratelimiter"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// ── Initialise dependencies ──────────────────────────────────────────────
	rl := ratelimiter.New()
	cs := catalog.New()

	rlHandler := handlers.NewRateLimitHandler(rl)
	catHandler := handlers.NewCatalogHandler(cs)

	// ── Register routes ──────────────────────────────────────────────────────
	mux := http.NewServeMux()

	// Part 1 – rate-limited request API
	mux.HandleFunc("/request", rlHandler.HandleRequest)
	mux.HandleFunc("/stats", rlHandler.HandleStats)

	// Part 2 – product catalog
	// /products          → list (GET) or create (POST)
	// /products/{id}     → detail (GET)
	// /products/{id}/media → add media (POST)
	mux.HandleFunc("/products", catHandler.HandleProducts)
	mux.HandleFunc("/products/", func(w http.ResponseWriter, r *http.Request) {
		// Guard: the bare "/products" path must not reach this handler.
		if r.URL.Path == "/products" || r.URL.Path == "/products/" {
			catHandler.HandleProducts(w, r)
			return
		}
		catHandler.HandleProductByID(w, r)
	})

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"status":"ok"}`)
	})

	// ── Logging middleware ───────────────────────────────────────────────────
	logged := loggingMiddleware(mux)

	addr := ":" + port
	log.Printf("Source Asia Backend listening on %s", addr)
	log.Printf("Endpoints:")
	log.Printf("  POST /request          – submit a rate-limited request")
	log.Printf("  GET  /stats            – view per-user rate-limit stats")
	log.Printf("  POST /products         – create a product")
	log.Printf("  GET  /products         – list products (paginated)")
	log.Printf("  GET  /products/{id}    – get product detail")
	log.Printf("  POST /products/{id}/media – add media to a product")
	log.Printf("  GET  /health           – health check")

	if err := http.ListenAndServe(addr, logged); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// loggingMiddleware logs every incoming request.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Wrap ResponseWriter to capture the status code.
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		log.Printf("%s %s → %d [%s]", r.Method, r.URL.Path, rw.status, strings.TrimPrefix(r.RemoteAddr, "[::1]"))
	})
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}
