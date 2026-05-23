#!/usr/bin/env bash
# seed.sh – creates 1,000 products via the API for performance testing.
# Usage: ./scripts/seed.sh [BASE_URL]
# Default BASE_URL: http://localhost:8080

set -euo pipefail

BASE="${1:-http://localhost:8080}"
TOTAL=1000

echo "Seeding $TOTAL products against $BASE ..."

for i in $(seq 1 $TOTAL); do
  SKU=$(printf "SKU-%04d" "$i")

  # Build image_urls array (10 per product)
  IMGS=""
  for j in $(seq 1 10); do
    IMGS="${IMGS}\"https://cdn.example.com/products/${SKU}/img-${j}.jpg\","
  done
  IMGS="[${IMGS%,}]"   # strip trailing comma, wrap in brackets

  # Build video_urls array (2 per product)
  VIDS="[\"https://cdn.example.com/products/${SKU}/demo.mp4\",\"https://cdn.example.com/products/${SKU}/overview.mp4\"]"

  BODY="{\"name\":\"Product ${i}\",\"sku\":\"${SKU}\",\"image_urls\":${IMGS},\"video_urls\":${VIDS}}"

  HTTP_STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
    -X POST "${BASE}/products" \
    -H "Content-Type: application/json" \
    -d "${BODY}")

  if [ "$HTTP_STATUS" != "201" ]; then
    echo "WARNING: product $i got HTTP $HTTP_STATUS"
  fi

  # Print progress every 100 products.
  if (( i % 100 == 0 )); then
    echo "  Created $i / $TOTAL"
  fi
done

echo "Done. Run: curl '${BASE}/products?limit=20&offset=0'"
