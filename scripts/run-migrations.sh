#!/bin/bash
set -e

# Simple migration runner using golang-migrate Docker image
# Usage: ./scripts/run-migrations.sh [up|down]

DIRECTION=${1:-up}
DATABASE_URL=${DATABASE_URL:-"postgresql://medspa:medspa@localhost:5432/medspa?sslmode=disable"}

echo "🔄 Running migrations ($DIRECTION)..."
echo "   Database: $DATABASE_URL"
echo ""

docker run --rm \
    --network host \
    -v "$(pwd)/migrations:/migrations" \
    migrate/migrate:v4.17.0 \
    -path=/migrations \
    -database "$DATABASE_URL" \
    $DIRECTION

echo "✅ Migrations completed"
