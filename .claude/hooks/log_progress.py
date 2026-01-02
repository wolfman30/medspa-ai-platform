#!/usr/bin/env python3
"""
Progress Logger Hook for Claude Code

Logs tool executions to a progress file for monitoring autonomous sessions.
Runs after each Bash tool execution.
"""

import json
import sys
import os
from datetime import datetime

PROGRESS_FILE = os.path.join(
    os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))),
    ".claude",
    "session_progress.log"
)

def main():
    try:
        input_data = json.load(sys.stdin)
    except:
        input_data = {}

    tool_name = input_data.get("tool_name", "unknown")
    tool_input = input_data.get("tool_input", {})

    # Extract command if it's a Bash tool
    command = ""
    if isinstance(tool_input, dict):
        command = tool_input.get("command", "")[:100]  # First 100 chars

    # Log entry
    timestamp = datetime.now().isoformat()
    log_entry = f"[{timestamp}] {tool_name}: {command}\n"

    try:
        os.makedirs(os.path.dirname(PROGRESS_FILE), exist_ok=True)
        with open(PROGRESS_FILE, "a") as f:
            f.write(log_entry)
    except:
        pass  # Don't fail on logging errors

    # Return empty response (don't affect tool execution)
    print(json.dumps({}))

if __name__ == "__main__":
    main()
