#!/usr/bin/env python3
"""
MVP Completion Check Hook for Claude Code

This hook runs when Claude tries to stop. It checks if the MVP acceptance tests
pass. If tests fail, it blocks stopping and tells Claude to continue working.

Returns JSON:
- {"decision": "approve"} - Allow Claude to stop (all tests pass)
- {"decision": "block", "reason": "..."} - Force Claude to continue
"""

import json
import subprocess
import sys
import os

def run_acceptance_tests():
    """Run MVP acceptance tests and return (passed, output)."""
    try:
        result = subprocess.run(
            ["go", "test", "-v", "./tests/..."],
            capture_output=True,
            text=True,
            timeout=300,  # 5 minute timeout
            cwd=os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
        )
        return result.returncode == 0, result.stdout + result.stderr
    except subprocess.TimeoutExpired:
        return False, "Tests timed out after 5 minutes"
    except FileNotFoundError:
        # go command not found - skip test check
        return True, "Go not found - skipping test check"
    except Exception as e:
        return False, f"Error running tests: {e}"

def main():
    # Read hook input from stdin
    try:
        input_data = json.load(sys.stdin)
    except:
        input_data = {}

    # Check if this is a natural conversation end or Claude is done with work
    transcript = input_data.get("transcript", "")

    # Run acceptance tests
    tests_pass, test_output = run_acceptance_tests()

    if tests_pass:
        # MVP complete - allow stop
        response = {
            "decision": "approve",
            "reason": "All MVP acceptance tests pass. Work complete."
        }
    else:
        # Tests failing - force continuation
        # Extract failing test names from output
        failing_tests = []
        for line in test_output.split("\n"):
            if "FAIL" in line or "--- FAIL" in line:
                failing_tests.append(line.strip())

        failure_summary = "\n".join(failing_tests[:5])  # First 5 failures

        response = {
            "decision": "block",
            "reason": f"MVP acceptance tests are failing. Please fix these issues before stopping:\n{failure_summary}\n\nRun 'go test -v ./tests/...' to see full output."
        }

    print(json.dumps(response))

if __name__ == "__main__":
    main()
