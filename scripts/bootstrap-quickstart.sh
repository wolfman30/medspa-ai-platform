#!/bin/bash
set -e

echo "🚀 MedSpa AI Platform - Bootstrap Quickstart"
echo "=============================================="
echo ""

# Check prerequisites
command -v docker >/dev/null 2>&1 || { echo "❌ Docker is required but not installed. Aborting." >&2; exit 1; }
command -v docker-compose >/dev/null 2>&1 || command -v docker compose >/dev/null 2>&1 || { echo "❌ Docker Compose is required but not installed. Aborting." >&2; exit 1; }

echo "✅ Prerequisites checked"
echo ""

# Check if .env exists
if [ ! -f .env ]; then
    echo "⚠️  No .env file found. Creating from .env.bootstrap.example..."
    cp .env.bootstrap.example .env
    echo ""
    echo "📝 IMPORTANT: Edit .env and add your API keys:"
    echo "   - BEDROCK_MODEL_ID (required)"
    echo "   - AWS_REGION + AWS credentials (required for Bedrock)"
    echo "   - TELNYX_API_KEY (required for SMS)"
    echo "   - SQUARE_ACCESS_TOKEN (required for payments)"
    echo ""
    echo "Press Enter after you've added your keys, or Ctrl+C to exit..."
    read -r
fi

# Validate critical environment variables
source .env
if [ -z "$BEDROCK_MODEL_ID" ]; then
    echo "❌ BEDROCK_MODEL_ID not set in .env"
    exit 1
fi
if [ -z "$AWS_REGION" ]; then
    echo "❌ AWS_REGION not set in .env"
    exit 1
fi
if [ -z "$AWS_ACCESS_KEY_ID" ] || [ -z "$AWS_SECRET_ACCESS_KEY" ]; then
    echo "WARNING: AWS credentials not set in .env; Bedrock calls may fail inside Docker containers."
fi

echo "✅ Environment configured"
echo ""

# Start local dependencies (Postgres + Redis)
echo "🐘 Starting local Postgres and Redis..."
docker compose up -d postgres redis

# Wait for Postgres to be ready
echo "⏳ Waiting for Postgres to be ready..."
until docker compose exec -T postgres pg_isready -U medspa >/dev/null 2>&1; do
    sleep 1
done

echo "✅ Postgres is ready"
echo ""

# Run migrations
echo "📊 Running database migrations..."
docker compose run --rm migrate -path=/migrations -database "postgresql://medspa:medspa@postgres:5432/medspa?sslmode=disable" up

echo "✅ Migrations completed"
echo ""

# Build and start the API
echo "🏗️  Building bootstrap container..."
docker compose -f docker-compose.bootstrap.yml build

echo ""
echo "🚀 Starting API with inline workers..."
docker compose -f docker-compose.bootstrap.yml up -d

# Wait for API to be healthy
echo "⏳ Waiting for API to be ready..."
sleep 3

MAX_RETRIES=30
RETRY_COUNT=0
until curl -f http://localhost:8080/health >/dev/null 2>&1; do
    RETRY_COUNT=$((RETRY_COUNT + 1))
    if [ $RETRY_COUNT -ge $MAX_RETRIES ]; then
        echo "❌ API failed to start. Check logs with: docker compose -f docker-compose.bootstrap.yml logs api"
        exit 1
    fi
    sleep 1
done

echo ""
echo "✅ Bootstrap stack is running!"
echo ""
echo "📍 Endpoints:"
echo "   - API: http://localhost:8080"
echo "   - Health: http://localhost:8080/health"
echo "   - Metrics: http://localhost:8080/metrics"
echo ""
echo "📋 Useful commands:"
echo "   - View logs: docker compose -f docker-compose.bootstrap.yml logs -f api"
echo "   - Stop stack: docker compose -f docker-compose.bootstrap.yml down"
echo "   - Restart: docker compose -f docker-compose.bootstrap.yml restart api"
echo "   - Shell: docker compose -f docker-compose.bootstrap.yml exec api sh"
echo ""
echo "🧪 Test the health endpoint:"
curl -s http://localhost:8080/health | jq . || curl -s http://localhost:8080/health
echo ""
