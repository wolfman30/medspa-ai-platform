#!/usr/bin/env bash
# CI Pre-deploy check: fails if very recent activity detected
# Used in GitHub Actions before deployment
#
# Required env vars:
#   DATABASE_URL - PostgreSQL connection string
#
# Optional env vars:
#   ACTIVITY_WINDOW_MINUTES - minutes to check (default: 5)
#   BLOCK_DEPLOY - if "true", exit 1 on activity; if "false", just warn (default: false)
#
# Exit codes:
#   0 = safe to deploy OR warn-only mode
#   1 = active conversations detected AND BLOCK_DEPLOY=true

set -euo pipefail

ACTIVITY_WINDOW_MINUTES="${ACTIVITY_WINDOW_MINUTES:-5}"
BLOCK_DEPLOY="${BLOCK_DEPLOY:-false}"

echo "::group::Pre-Deploy Activity Check"
echo "Checking for conversations in the last ${ACTIVITY_WINDOW_MINUTES} minutes..."

if [ -z "${DATABASE_URL:-}" ]; then
    echo "::warning::DATABASE_URL not set, skipping pre-deploy check"
    echo "::endgroup::"
    exit 0
fi

# Query for very recent activity
RECENT_COUNT=$(psql "${DATABASE_URL}" -t -A -c "
    SELECT COUNT(*) FROM leads
    WHERE updated_at > NOW() - INTERVAL '${ACTIVITY_WINDOW_MINUTES} minutes';
" 2>&1) || {
    echo "::warning::Failed to connect to database for pre-deploy check"
    echo "::endgroup::"
    exit 0  # Don't block deploy on DB connection issues
}

echo "Active leads in last ${ACTIVITY_WINDOW_MINUTES} min: ${RECENT_COUNT}"

if [ "${RECENT_COUNT}" -gt 0 ]; then
    echo ""
    echo "::warning::${RECENT_COUNT} lead(s) active in the last ${ACTIVITY_WINDOW_MINUTES} minutes - someone may be mid-conversation"

    if [ "${BLOCK_DEPLOY}" = "true" ]; then
        echo "::error::Blocking deployment due to active conversations (BLOCK_DEPLOY=true)"
        echo "::endgroup::"
        exit 1
    else
        echo "Continuing with deployment (BLOCK_DEPLOY=false, warn-only mode)"
    fi
else
    echo "✅ No recent activity - safe to deploy"
fi

echo "::endgroup::"
exit 0
