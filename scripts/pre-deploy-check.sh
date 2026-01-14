#!/usr/bin/env bash
# Pre-deploy check: warns if there are active conversations in the last N minutes
#
# Usage (local):
#   DATABASE_URL="postgresql://..." ./scripts/pre-deploy-check.sh
#
# Usage (with SSM tunnel already open):
#   DATABASE_URL="postgresql://medspa:xxx@localhost:5432/medspa" ./scripts/pre-deploy-check.sh
#
# Exit codes:
#   0 = safe to deploy (no recent activity)
#   1 = active conversations detected (deploy with caution)
#   2 = error (couldn't connect to database)

set -euo pipefail

# Configuration
ACTIVITY_WINDOW_MINUTES="${ACTIVITY_WINDOW_MINUTES:-30}"
WARN_THRESHOLD="${WARN_THRESHOLD:-1}"

# Colors
RED='\033[0;31m'
YELLOW='\033[1;33m'
GREEN='\033[0;32m'
NC='\033[0m' # No Color

echo "============================================"
echo "  Pre-Deploy Activity Check"
echo "============================================"
echo ""
echo "Checking for conversations in the last ${ACTIVITY_WINDOW_MINUTES} minutes..."
echo ""

if [ -z "${DATABASE_URL:-}" ]; then
    echo -e "${RED}ERROR: DATABASE_URL environment variable not set${NC}"
    echo ""
    echo "Set it to your dev database connection string:"
    echo "  export DATABASE_URL=\"postgresql://user:pass@host:5432/medspa\""
    echo ""
    echo "Or use SSM tunnel first:"
    echo "  aws ssm start-session --target <bastion-instance-id> --document-name AWS-StartPortForwardingSession --parameters '{\"portNumber\":[\"5432\"],\"localPortNumber\":[\"5432\"]}'"
    exit 2
fi

# Query for recent conversation activity
# Checks both leads (updated_at) and any recent message activity
QUERY="
SELECT
    (SELECT COUNT(*) FROM leads WHERE updated_at > NOW() - INTERVAL '${ACTIVITY_WINDOW_MINUTES} minutes') as recent_leads,
    (SELECT COUNT(*) FROM leads WHERE updated_at > NOW() - INTERVAL '5 minutes') as very_recent_leads;
"

RESULT=$(psql "${DATABASE_URL}" -t -A -F',' -c "${QUERY}" 2>&1) || {
    echo -e "${RED}ERROR: Failed to connect to database${NC}"
    echo "${RESULT}"
    exit 2
}

RECENT_LEADS=$(echo "${RESULT}" | cut -d',' -f1)
VERY_RECENT_LEADS=$(echo "${RESULT}" | cut -d',' -f2)

echo "Results:"
echo "  - Leads active in last ${ACTIVITY_WINDOW_MINUTES} min: ${RECENT_LEADS}"
echo "  - Leads active in last 5 min: ${VERY_RECENT_LEADS}"
echo ""

# Decision logic
if [ "${VERY_RECENT_LEADS}" -ge "${WARN_THRESHOLD}" ]; then
    echo -e "${RED}⚠️  WARNING: ${VERY_RECENT_LEADS} lead(s) active in the last 5 minutes!${NC}"
    echo ""
    echo "Someone may be mid-conversation RIGHT NOW."
    echo "Recommendation: Wait 10-15 minutes before deploying."
    echo ""

    # Show who's active
    echo "Active leads:"
    psql "${DATABASE_URL}" -c "
        SELECT id, phone, name, created_at, updated_at
        FROM leads
        WHERE updated_at > NOW() - INTERVAL '5 minutes'
        ORDER BY updated_at DESC
        LIMIT 5;
    " 2>/dev/null || true

    echo ""
    read -p "Deploy anyway? (y/N): " -n 1 -r
    echo ""
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "Deployment cancelled."
        exit 1
    fi
    echo -e "${YELLOW}Proceeding with deployment (user override)...${NC}"
    exit 0

elif [ "${RECENT_LEADS}" -ge "${WARN_THRESHOLD}" ]; then
    echo -e "${YELLOW}⚡ CAUTION: ${RECENT_LEADS} lead(s) active in the last ${ACTIVITY_WINDOW_MINUTES} minutes${NC}"
    echo ""
    echo "Recent activity detected, but no one is actively mid-conversation."
    echo "Deployment should be safe, but be ready to monitor."
    echo ""
    exit 0

else
    echo -e "${GREEN}✅ SAFE TO DEPLOY: No recent conversation activity${NC}"
    echo ""
    exit 0
fi
