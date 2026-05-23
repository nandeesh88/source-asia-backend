

A single Go HTTP service implementing two parts:

1. **Rate-limited request API** (`POST /request`, `GET /stats`)  
2. **Product catalog with media** (`/products` CRUD)

---

## Quick Start

```bash
# Clone / unzip, then:
cd source-asia-backend
go run ./cmd/server          # starts on :8080

# Or build a binary first:
go build -o server ./cmd/server
./server

# Override port:
PORT=9090 go run ./cmd/server
```

**Requirements:** Go 1.22+ (no external dependencies — stdlib only)

---

## Running Tests

```bash
go test ./...                    # all tests
go test -race ./...              # with race detector (recommended)
go test -v ./internal/ratelimiter/...
go test -v ./internal/catalog/...
```

---

## Part 1 – Rate-Limited API

### Design decisions

| Decision | Choice | Reason |
|---|---|---|
| Success status | **201 Created** | A new "request record" is created; 201 is semantically correct |
| Window type | **Fixed 1-minute window** | Simpler to reason about; documented below |
| Rejected counter | **Cumulative (all-time)** | Easier to audit; window-based accepted count is also returned |
| Concurrency | `sync.RWMutex` on the user map | Writers acquire full write-lock; readers use read-lock for stats |

**Fixed window:** the window for a user starts at the moment of their first request and expires exactly 60 seconds later.  After expiry the counter resets.  A rolling (sliding) window would be more accurate but requires a per-user ring buffer; fixed windows are the standard choice for most production rate limiters.

---

### `POST /request`

**Request body**

```json
{
  "user_id": "alice",
  "payload": { "any": "json value" }
}
```

**Success – 201 Created**

```json
{
  "status": "accepted",
  "user_id": "alice",
  "message": "request has been accepted and will be processed",
  "timestamp": "2025-01-15T10:30:00Z"
}
```

**Rate limited – 429 Too Many Requests**

```json
{
  "error": "rate limit exceeded",
  "details": "maximum 5 requests per minute per user; please wait before retrying"
}
```

**Bad input – 400 Bad Request**

```json
{
  "error": "user_id is required and must not be empty",
  "details": ""
}
```

---

### `GET /stats`

```json
{
  "users": [
    {
      "user_id": "alice",
      "accepted_in_window": 3,
      "rejected_total": 1,
      "window_starts_at": "2025-01-15T10:30:00Z",
      "window_ends_at":   "2025-01-15T10:31:00Z"
    }
  ],
  "updated_at": "2025-01-15T10:30:45Z"
}
```

| Field | Description |
|---|---|
| `accepted_in_window` | Accepted requests in the **current** 1-minute window |
| `rejected_total` | **Cumulative** rejections across all windows, all time |
| `window_starts_at` / `window_ends_at` | The open/close time of the user's current window |

---

### Part 1 – curl examples

```bash
# Accept 5 requests
for i in 1 2 3 4 5; do
  curl -s -X POST http://localhost:8080/request \
    -H "Content-Type: application/json" \
    -d '{"user_id":"alice","payload":{"order_id":'$i'}}' | jq .
done

# 6th request → 429
curl -s -X POST http://localhost:8080/request \
  -H "Content-Type: application/json" \
  -d '{"user_id":"alice","payload":{}}' | jq .

# View stats
curl -s http://localhost:8080/stats | jq .

# Bad request (missing user_id)
curl -s -X POST http://localhost:8080/request \
  -H "Content-Type: application/json" \
  -d '{"payload":"hello"}' | jq .
```

---

### Production limitations (Part 1)

| Limitation | Description |
|---|---|
| **Single instance only** | Rate-limit state lives in the Go process heap; two instances have independent counters and together allow 10 req/min per user |
| **Restart = state reset** | Window counters and rejection history are lost on restart |
| **No persistence** | Cannot audit historical request counts |
| **Multi-instance deployment** | Requires a shared external store (Redis with `INCR` + `EXPIRE`, or a distributed rate-limiter sidecar) |
| **No user authentication** | Any caller can use any `user_id` string |

---

## Part 2 – Product Catalog

### Data model

Products are stored in a single `map[string]*Product` keyed by ID.

```
Store
├── products  map[id → *Product]   full struct with all URL slices
├── skuIndex  map[sku → id]        fast duplicate-SKU detection  O(1)
└── order     []string             insertion-order IDs for stable pagination
```

### List vs Detail queries

| Query | What is accessed |
|---|---|
| `GET /products` | Iterates `order[offset:offset+limit]`, reads only `ID, Name, SKU, CreatedAt` + `len(ImageURLs)` / `len(VideoURLs)` per product. URL strings are **never allocated or serialised**. |
| `GET /products/{id}` | Reads the single `*Product` and deep-copies both URL slices. |

With 1,000 products × 10 images, `GET /products?limit=20` touches 20 products and reads 20 integer lengths — it never serialises 10,000 URL strings.

### Production (PostgreSQL + CDN)

```sql
-- products table (scalar fields only)
CREATE TABLE products (
  id         UUID PRIMARY KEY,
  name       TEXT NOT NULL,
  sku        TEXT UNIQUE NOT NULL,
  created_at TIMESTAMPTZ DEFAULT now()
);

-- media table (separate, not joined in list query)
CREATE TABLE product_media (
  id         BIGSERIAL PRIMARY KEY,
  product_id UUID REFERENCES products(id) ON DELETE CASCADE,
  kind       TEXT CHECK (kind IN ('image','video')),
  url        TEXT NOT NULL,
  position   INT  NOT NULL DEFAULT 0
);

-- List query: COUNT aggregates, no URL strings loaded
SELECT p.id, p.name, p.sku, p.created_at,
       COUNT(CASE WHEN m.kind='image' THEN 1 END) AS image_count,
       COUNT(CASE WHEN m.kind='video' THEN 1 END) AS video_count,
       MIN(CASE WHEN m.kind='image' AND m.position=0 THEN m.url END) AS thumbnail_url
FROM products p
LEFT JOIN product_media m ON m.product_id = p.id
GROUP BY p.id
ORDER BY p.created_at DESC
LIMIT $1 OFFSET $2;

-- Detail query: separate second query for media URLs
SELECT kind, url FROM product_media WHERE product_id=$1 ORDER BY kind, position;
```

CDN URLs are stored as plain strings; actual file hosting is outside this service.  A pre-signed URL or CDN path rewrite layer would sit in front.

---

### Endpoints

#### `POST /products` – Create product

```bash
curl -s -X POST http://localhost:8080/products \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Widget A",
    "sku": "SKU-0001",
    "image_urls": [
      "https://cdn.example.com/products/sku-0001/img-1.jpg",
      "https://cdn.example.com/products/sku-0001/img-2.jpg"
    ],
    "video_urls": [
      "https://cdn.example.com/products/sku-0001/demo.mp4"
    ]
  }' | jq .
```

**Response – 201 Created**

```json
{
  "id": "prod_000001",
  "name": "Widget A",
  "sku": "SKU-0001",
  "image_urls": ["https://cdn.example.com/products/sku-0001/img-1.jpg", "..."],
  "video_urls": ["https://cdn.example.com/products/sku-0001/demo.mp4"],
  "created_at": "2025-01-15T10:30:00Z"
}
```

Duplicate SKU → **409 Conflict**

---

#### `GET /products` – List products

```bash
curl -s "http://localhost:8080/products?limit=10&offset=0" | jq .
```

**Query parameters**

| Parameter | Default | Max | Description |
|---|---|---|---|
| `limit` | 20 | 100 | Items per page |
| `offset` | 0 | — | Zero-based skip count |

**Response – 200 OK**

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
  "limit": 10,
  "offset": 0
}
```

Note: `image_urls` and `video_urls` are deliberately absent from list items.

---

#### `GET /products/{id}` – Get product detail

```bash
curl -s http://localhost:8080/products/prod_000001 | jq .
```

Returns full product including all URL arrays. `404` if not found.

---

#### `POST /products/{id}/media` – Add media

```bash
curl -s -X POST http://localhost:8080/products/prod_000001/media \
  -H "Content-Type: application/json" \
  -d '{
    "image_urls": ["https://cdn.example.com/products/sku-0001/img-3.jpg"],
    "video_urls": ["https://cdn.example.com/products/sku-0001/tour.mp4"]
  }' | jq .
```

Returns the updated full product. `404` if unknown id. `400` if no URLs given.

---

### Validation rules

| Rule | Detail |
|---|---|
| `name` | Required, non-empty after trimming whitespace |
| `sku` | Required, non-empty, unique across all products |
| URL scheme | Must be `http://` or `https://` |
| URL length | Maximum 2,048 characters |
| URL format | Must have a valid host |
| Max URLs per request | 20 per `image_urls`, 20 per `video_urls` |

---

### Seeding 1,000 products (optional performance test)

```bash
# Start the server first, then:
bash scripts/seed.sh

# Or point at a different host:
bash scripts/seed.sh http://localhost:9090

# After seeding, verify the list query is fast and correct:
curl -s "http://localhost:8080/products?limit=20&offset=0" | jq '.total, (.products | length)'
# → 1000
# → 20
```

---

## AI Usage

Claude (Anthropic) was used to assist in writing this implementation. All code was reviewed for correctness, concurrency safety, and alignment with the assignment requirements.

---

## Repository structure

```
source-asia-backend/
├── cmd/
│   └── server/
│       └── main.go          # entry point, route wiring
├── internal/
│   ├── models/
│   │   └── models.go        # all request/response types
│   ├── ratelimiter/
│   │   ├── ratelimiter.go   # Part 1 logic
│   │   └── ratelimiter_test.go
│   ├── catalog/
│   │   ├── catalog.go       # Part 2 store
│   │   └── catalog_test.go
│   └── handlers/
│       ├── ratelimit.go     # HTTP handlers – Part 1
│       ├── catalog.go       # HTTP handlers – Part 2
│       └── helpers.go       # shared JSON helpers
├── scripts/
│   └── seed.sh              # optional 1,000-product seeder
├── go.mod
└── README.md
```
