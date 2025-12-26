#!/usr/bin/env bash
set -euo pipefail

# =============================================================================
# MedSpa AI Platform - E2E Test Runner
# =============================================================================
#
# This script runs the full end-to-end test with sensible defaults.
#
# Usage:
#   ./scripts/run-e2e-test.sh              # Run with defaults
#   ./scripts/run-e2e-test.sh --quick      # Run with shorter waits
#   ./scripts/run-e2e-test.sh --no-db      # Skip database checks
#
# Environment variables:
#   API_URL              - API endpoint (default: http://localhost:8082)
#   DATABASE_URL         - PostgreSQL connection string
#   TEST_ORG_ID          - Organization ID for testing
#   TEST_CUSTOMER_PHONE  - Simulated customer phone
#   TEST_CLINIC_PHONE    - Clinic's hosted phone number
#
# =============================================================================

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Defaults
export API_URL="${API_URL:-http://localhost:8082}"
export AI_RESPONSE_WAIT="${AI_RESPONSE_WAIT:-8}"
export STEP_DELAY="${STEP_DELAY:-2}"
export SKIP_DB_CHECK="${SKIP_DB_CHECK:-}"

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --quick)
            export AI_RESPONSE_WAIT=3
            export STEP_DELAY=1
            shift
            ;;
        --no-db)
            export SKIP_DB_CHECK=1
            shift
            ;;
        --help)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --quick    Use shorter wait times (3s instead of 8s)"
            echo "  --no-db    Skip database verification steps"
            echo "  --help     Show this help message"
            echo ""
            echo "Environment Variables:"
            echo "  API_URL              API endpoint (default: http://localhost:8082)"
            echo "  DATABASE_URL         PostgreSQL connection string"
            echo "  TEST_ORG_ID          Organization ID for testing"
            echo "  TEST_CUSTOMER_PHONE  Customer phone number"
            echo "  TEST_CLINIC_PHONE    Clinic phone number"
            exit 0
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            echo "Use --help for usage information"
            exit 1
            ;;
    esac
done

echo ""
echo "=============================================="
echo "  MedSpa AI Platform - E2E Test Runner"
echo "=============================================="
echo ""

# Check if Python is available
if ! command -v python3 &> /dev/null; then
    if ! command -v python &> /dev/null; then
        echo -e "${RED}Error: Python is required but not found${NC}"
        exit 1
    fi
    PYTHON_CMD="python"
else
    PYTHON_CMD="python3"
fi

# Check if requests module is available
if ! $PYTHON_CMD -c "import requests" 2>/dev/null; then
    echo -e "${YELLOW}Installing 'requests' module...${NC}"
    $PYTHON_CMD -m pip install requests --quiet
fi

# Check if API is reachable first
echo -n "Checking API availability... "
if curl -s --max-time 5 "${API_URL}/health" > /dev/null 2>&1; then
    echo -e "${GREEN}OK${NC}"
else
    echo -e "${RED}FAILED${NC}"
    echo ""
    echo -e "${YELLOW}API is not reachable at ${API_URL}${NC}"
    echo ""
    echo "Please ensure the API is running. Start it with:"
    echo ""
    echo "  # Option 1: Docker Compose"
    echo "  docker compose up -d"
    echo ""
    echo "  # Option 2: Direct run"
    echo "  go run cmd/api/main.go"
    echo ""
    exit 1
fi

# Run the E2E test
cd "$PROJECT_ROOT"
echo ""
exec $PYTHON_CMD scripts/e2e_full_flow.py
