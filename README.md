# Source Asia Backend Assignment

A single Go HTTP service with no external dependencies, implementing a rate-limited request API and a product catalog with media management.

---

## Tech Stack

- Language: Go 1.22
- Storage: In-memory (maps, slices)
- Dependencies: Standard library only

---

## Project Structure

```
source-asia-backend/
    cmd/server/main.go                  entry point and route registration
    internal/models/models.go           request and response types
    internal/ratelimiter/
        ratelimiter.go                  fixed-window rate limiter
        ratelimiter_test.go
    internal/catalog/
        catalog.go                      product store with validation and pagination
        catalog_test.go
    internal/handlers/
        ratelimit.go                    POST /request, GET /stats
        catalog.go                      all /products endpoints
        helpers.go                      shared JSON response helpers
    scripts/seed.sh                     optional script to seed 1,000 products
```

---

## Getting Started

Requirements: Go 1.22 or later

```bash
cd source-asia-backend
go run ./cmd/server
```

Server starts on port 8080. To use a different port:

```bash
PORT=9090 go run ./cmd/server
```

To build a binary:

```bash
go build -o server ./cmd/server
./server
```

---

## Running Tests

```bash
go test ./...
go test -race ./...
```

16 tests across the rate limiter and catalog packages, including a concurrency test that fires 50 parallel requests and asserts the accepted count never exceeds the limit.

---

## Part 1 - Rate-Limited Request API

### How it works

Each user gets a fixed 1-minute window starting from their first request. Up to 5 requests are accepted within that window. On the 6th request, the server returns 429. After 60 seconds, the window resets and the counter starts fresh.

Concurrency is handled with a `sync.RWMutex`. All counter increments happen inside a write lock, so parallel requests for the same user cannot race past the limit.

Rejected count is cumulative across all windows. Accepted count reflects the current window only.

### Design Decisions

| Decision | Choice |
|---|---|
| Success status code | 201 Created — a request record is being created |
| Window type | Fixed 1-minute window |
| Rejected counter | Cumulative all-time |
| Concurrency control | sync.RWMutex on the user map |

### POST /request

Request body:
```json
{
  "user_id": "alice",
  "payload": { "any": "json value" }
}
```

Success response (201):
```json
{
  "status": "accepted",
  "user_id": "alice",
  "message": "request has been accepted and will be processed",
  "timestamp": "2025-01-15T10:30:00Z"
}
```

Rate limited (429):
```json
{
  "error": "rate limit exceeded",
  "details": "maximum 5 requests per minute per user; please wait before retrying"
}
```

Invalid input (400):
```json
{
  "error": "user_id is required and must not be empty",
  "details": ""
}
```

### GET /stats

Response (200):
```json
{
  "users": [
    {
      "user_id": "alice",
      "accepted_in_window": 3,
      "rejected_total": 1,
      "window_starts_at": "2025-01-15T10:30:00Z",
      "window_ends_at": "2025-01-15T10:31:00Z"
    }
  ],
  "updated_at": "2025-01-15T10:30:45Z"
}
```

`accepted_in_window` reflects the current 1-minute window. `rejected_total` is cumulative across all time.

### curl Examples

```bash
# Send 5 accepted requests
for i in 1 2 3 4 5; do
  curl -s -X POST http://localhost:8080/request \
    -H "Content-Type: application/json" \
    -d "{\"user_id\":\"alice\",\"payload\":{\"order_id\":$i}}"
done

# 6th request — expect 429
curl -s -X POST http://localhost:8080/request \
  -H "Content-Type: application/json" \
  -d '{"user_id":"alice","payload":{}}'

# Check stats
curl -s http://localhost:8080/stats

# Invalid request — expect 400
curl -s -X POST http://localhost:8080/request \
  -H "Content-Type: application/json" \
  -d '{"payload":"hello"}'
```

### Production Limitations

| Limitation | Detail |
|---|---|
| Single instance only | State lives in the Go process. Two instances each allow 5 req/min, effectively doubling the limit. |
| Restart loses state | All counters and window timers are reset on restart. |
| No persistence | Request history cannot be audited after the fact. |
| Multi-instance deployment | Requires a shared external store such as Redis using INCR and EXPIRE commands. |
| No authentication | Any caller can pass any user_id string. |

---

## Part 2 - Product Catalog with Media

### Data Model

```
Store
    products    map[id] to *Product     full struct including all URL slices
    skuIndex    map[sku] to id          O(1) duplicate SKU detection
    order       []string                insertion-order IDs for stable pagination
```

### List vs Detail

GET /products iterates only the IDs in the requested page window. For each product it reads the scalar fields (id, name, sku, created_at) and the lengths of the URL slices. The URL strings themselves are never allocated or serialised in the list response.

GET /products/{id} reads the single product and returns the full URL arrays.

With 1,000 products and 10 images each, a request for `GET /products?limit=20` touches 20 products and reads 20 integer lengths. It does not load or serialise the 10,000 stored URL strings.

### Production Design (PostgreSQL + CDN)

In production, products and media would be stored in separate tables. The list query would use COUNT aggregates against the media table — never fetching URL strings. The detail query would run a second targeted query to fetch media rows for that one product.

```sql
CREATE TABLE products (
  id         UUID PRIMARY KEY,
  name       TEXT NOT NULL,
  sku        TEXT UNIQUE NOT NULL,
  created_at TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE product_media (
  id         BIGSERIAL PRIMARY KEY,
  product_id UUID REFERENCES products(id) ON DELETE CASCADE,
  kind       TEXT CHECK (kind IN ('image', 'video')),
  url        TEXT NOT NULL,
  position   INT NOT NULL DEFAULT 0
);

-- List query: aggregates only, no URL strings loaded
SELECT p.id, p.name, p.sku, p.created_at,
       COUNT(CASE WHEN m.kind = 'image' THEN 1 END) AS image_count,
       COUNT(CASE WHEN m.kind = 'video' THEN 1 END) AS video_count,
       MIN(CASE WHEN m.kind = 'image' AND m.position = 0 THEN m.url END) AS thumbnail_url
FROM products p
LEFT JOIN product_media m ON m.product_id = p.id
GROUP BY p.id
ORDER BY p.created_at DESC
LIMIT $1 OFFSET $2;

-- Detail query: fetch media separately for the requested product only
SELECT kind, url FROM product_media WHERE product_id = $1 ORDER BY kind, position;
```

### POST /products

Request body:
```json
{
  "name": "Widget A",
  "sku": "SKU-0001",
  "image_urls": [
    "https://cdn.example.com/products/sku-0001/img-1.jpg",
    "https://cdn.example.com/products/sku-0001/img-2.jpg"
  ],
  "video_urls": [
    "https://cdn.example.com/products/sku-0001/demo.mp4"
  ]
}
```

Success response (201):
```json
{
  "id": "prod_000001",
  "name": "Widget A",
  "sku": "SKU-0001",
  "image_urls": ["https://cdn.example.com/products/sku-0001/img-1.jpg"],
  "video_urls": ["https://cdn.example.com/products/sku-0001/demo.mp4"],
  "created_at": "2025-01-15T10:30:00Z"
}
```

Duplicate SKU returns 409 Conflict.

### GET /products

Query parameters:

| Parameter | Default | Max |
|---|---|---|
| limit | 20 | 100 |
| offset | 0 | — |

Response (200):
```json
{
  "products": [
    {
      "id": "prod_000001",
      "name": "Widget A",
      "sku": "SKU-0001",
      "image_count": 2,
      "video_count": 1,
      "thumbnail_url": "https://cdn.example.com/products/sku-0001/img-1.jpg",
      "created_at": "2025-01-15T10:30:00Z"
    }
  ],
  "total": 1,
  "limit": 20,
  "offset": 0
}
```

`image_urls` and `video_urls` are intentionally excluded from list items.

### GET /products/{id}

Returns the full product with all image and video URLs. Returns 404 if the id does not exist.

```bash
curl -s http://localhost:8080/products/prod_000001
```

### POST /products/{id}/media

Appends new URLs to an existing product. At least one of `image_urls` or `video_urls` must be provided. Returns 404 if the product does not exist, 400 if the body is empty.

```bash
curl -s -X POST http://localhost:8080/products/prod_000001/media \
  -H "Content-Type: application/json" \
  -d '{
    "image_urls": ["https://cdn.example.com/products/sku-0001/img-3.jpg"]
  }'
```

### Validation Rules

| Field | Rule |
|---|---|
| name | Required, non-empty after trimming whitespace |
| sku | Required, non-empty, unique across all products |
| URL scheme | Must be http or https |
| URL length | Maximum 2,048 characters |
| URLs per request | Maximum 20 per image_urls array, 20 per video_urls array |

### Seed Script (Optional Performance Test)

```bash
# Start the server, then run:
bash scripts/seed.sh

# Creates 1,000 products with 10 images and 2 videos each.
# After seeding, verify the list endpoint is fast:
curl -s "http://localhost:8080/products?limit=20&offset=0"
```

---

## AI Usage

Claude (Anthropic) was used to generate the initial code structure, implement the rate limiter and catalog logic, and write tests. GitHub Copilot was used within VS Code for debugging, inline suggestions, and refining implementation details. All generated code was reviewed for correctness and alignment with the assignment requirements.