#!/bin/bash
#
# Autonomous MVP Build Script for Claude Code
#
# This script runs Claude Code in headless mode to complete the MVP.
# It will continue until all acceptance tests pass or max-turns is reached.
#
# Usage:
#   ./scripts/run-autonomous.sh              # Default: 100 turns
#   ./scripts/run-autonomous.sh --max-turns 200   # Custom turn limit
#   ./scripts/run-autonomous.sh --continue   # Resume previous session
#

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LOG_DIR="$PROJECT_ROOT/.claude/logs"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
LOG_FILE="$LOG_DIR/autonomous_${TIMESTAMP}.log"

# Default settings
MAX_TURNS=${MAX_TURNS:-100}
CONTINUE_SESSION=""

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --max-turns)
            MAX_TURNS="$2"
            shift 2
            ;;
        --continue)
            CONTINUE_SESSION="--continue"
            shift
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Create log directory
mkdir -p "$LOG_DIR"

echo "========================================"
echo "MedSpa AI Platform - Autonomous Build"
echo "========================================"
echo "Project: $PROJECT_ROOT"
echo "Max turns: $MAX_TURNS"
echo "Log file: $LOG_FILE"
echo "Started: $(date)"
echo "========================================"

# Change to project directory
cd "$PROJECT_ROOT"

# Run initial test check
echo ""
echo "Running initial acceptance test check..."
if go test -v ./tests/... 2>&1 | head -50; then
    echo ""
    echo "Note: Some tests may already pass. Claude will verify and complete any gaps."
fi

echo ""
echo "Starting autonomous Claude Code session..."
echo ""

# Build the prompt
PROMPT=$(cat <<'EOF'
Complete the MedSpa AI Platform MVP by following these steps:

1. First, run the acceptance tests to see current status:
   go test -v ./tests/...

2. For each failing test, fix the underlying issue.

3. Check docs/MVP_STATUS.md for remaining work items.

4. Implement missing features with proper tests.

5. Run tests after each significant change.

6. Update docs/MVP_STATUS.md when completing items.

7. Continue until ALL acceptance tests pass.

The definition of done is: `go test -v ./tests/...` passes with 0 failures.

Start by running the acceptance tests to understand the current state.
EOF
)

# Run Claude Code in headless mode
# Note: This requires Claude Code CLI to be installed
if command -v claude &> /dev/null; then
    claude -p "$PROMPT" \
        --allowedTools "Read,Write,Edit,Bash,Glob,Grep" \
        --max-turns "$MAX_TURNS" \
        --output-format stream-json \
        $CONTINUE_SESSION \
        2>&1 | tee "$LOG_FILE"
else
    echo "ERROR: Claude Code CLI not found."
    echo ""
    echo "Install it with:"
    echo "  npm install -g @anthropic-ai/claude-code"
    echo ""
    echo "Or run manually in Claude Code with this prompt:"
    echo ""
    echo "$PROMPT"
    exit 1
fi

echo ""
echo "========================================"
echo "Autonomous session completed"
echo "Log saved to: $LOG_FILE"
echo "Ended: $(date)"
echo "========================================"

# Run final test check
echo ""
echo "Final acceptance test results:"
go test -v ./tests/...
