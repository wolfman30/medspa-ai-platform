#!/usr/bin/env python3
"""
MedSpa AI Platform - Development Orchestrator Integration

This script provides a bridge to the AI Agent Orchestrator for coordinating
development tasks. It can be used to:

1. Run predefined MedSpa workflows
2. Execute ad-hoc development tasks with appropriate agent routing
3. Validate implementations against workflow specifications
4. Generate tests for implemented features

Usage:
    python scripts/orchestrate.py workflow <name>       # Run a MedSpa workflow
    python scripts/orchestrate.py task <description>   # Execute a single task
    python scripts/orchestrate.py validate             # Validate all implementations
    python scripts/orchestrate.py test <component>     # Generate tests for component

Examples:
    python scripts/orchestrate.py workflow liability_guardrails
    python scripts/orchestrate.py task "Add rate limiting to the booking API"
    python scripts/orchestrate.py validate
    python scripts/orchestrate.py test internal/payments/refund.go
"""

import argparse
import os
import subprocess
import sys
from pathlib import Path

# Paths
SCRIPT_DIR = Path(__file__).parent
PROJECT_ROOT = SCRIPT_DIR.parent
ORCHESTRATOR_ROOT = PROJECT_ROOT.parent / "ai-agent-orchestrator"
ORCHESTRATOR_VENV = ORCHESTRATOR_ROOT / "venv" / "Scripts" / "agent-orchestrator"

# Available MedSpa workflows
MEDSPA_WORKFLOWS = [
    "liability_guardrails",
    "financial_safeguards",
    "staff_notifications",
    "onboarding_10dlc",
    "admin_dashboard",
]

# Task type to agent mapping for common MedSpa tasks
TASK_ROUTING = {
    "api": "coder",
    "endpoint": "coder",
    "handler": "coder",
    "service": "coder",
    "database": "coder",
    "migration": "coder",
    "test": "qa",
    "review": "architect",
    "design": "architect",
    "deploy": "devops",
    "infrastructure": "devops",
    "compliance": "compliance",
    "hipaa": "compliance",
    "disclaimer": "compliance",
    "audit": "compliance",
    "refund": "support",
    "dispute": "support",
    "escalation": "support",
    "notification": "business",
    "sla": "business",
    "onboarding": "business",
}


def run_orchestrator_command(args: list[str]) -> int:
    """Run an orchestrator command and return exit code."""
    if not ORCHESTRATOR_VENV.exists():
        print(f"Error: Orchestrator not found at {ORCHESTRATOR_ROOT}")
        print("Please set up the ai-agent-orchestrator first.")
        return 1

    cmd = [str(ORCHESTRATOR_VENV)] + args
    print(f"Running: {' '.join(cmd)}")
    print("-" * 60)

    result = subprocess.run(
        cmd,
        cwd=str(ORCHESTRATOR_ROOT),
        env={**os.environ, "WORKSPACE_PATH": str(PROJECT_ROOT)},
    )
    return result.returncode


def detect_agent_for_task(description: str) -> str | None:
    """Detect the appropriate agent based on task description."""
    description_lower = description.lower()
    for keyword, agent in TASK_ROUTING.items():
        if keyword in description_lower:
            return agent
    return None


def cmd_workflow(args):
    """Run a MedSpa workflow."""
    if args.name not in MEDSPA_WORKFLOWS:
        print(f"Error: Unknown workflow '{args.name}'")
        print(f"Available workflows: {', '.join(MEDSPA_WORKFLOWS)}")
        return 1

    cmd_args = ["run", args.name, "-w", str(PROJECT_ROOT)]
    if args.context:
        cmd_args.extend(["-c", args.context])

    return run_orchestrator_command(cmd_args)


def cmd_task(args):
    """Execute a single development task."""
    cmd_args = ["task", args.description]

    if args.type:
        cmd_args.extend(["-t", args.type])

    if args.agent:
        cmd_args.extend(["-a", args.agent])
    else:
        # Auto-detect agent
        detected = detect_agent_for_task(args.description)
        if detected:
            print(f"Auto-detected agent: {detected}")
            cmd_args.extend(["-a", detected])

    if args.output:
        cmd_args.extend(["-o", args.output])

    return run_orchestrator_command(cmd_args)


def cmd_validate(args):
    """Validate all workflow implementations."""
    # Use our validation script
    validate_script = ORCHESTRATOR_ROOT / "scripts" / "validate_medspa.py"
    if not validate_script.exists():
        print("Error: Validation script not found")
        return 1

    python_exe = ORCHESTRATOR_ROOT / "venv" / "Scripts" / "python"
    cmd = [str(python_exe), str(validate_script), str(PROJECT_ROOT)]

    result = subprocess.run(cmd, cwd=str(ORCHESTRATOR_ROOT))
    return result.returncode


def cmd_test(args):
    """Generate tests for a component."""
    component = args.component
    component_path = PROJECT_ROOT / component

    if not component_path.exists():
        print(f"Error: Component not found: {component}")
        return 1

    description = f"Write comprehensive unit tests for {component}. Include table-driven tests, edge cases, and mock any external dependencies."

    cmd_args = ["task", description, "-t", "test", "-a", "qa"]
    if args.output:
        cmd_args.extend(["-o", args.output])
    else:
        # Default output next to the source file
        test_dir = component_path.parent
        cmd_args.extend(["-o", str(test_dir)])

    return run_orchestrator_command(cmd_args)


def cmd_list(args):
    """List available workflows."""
    print("Available MedSpa Workflows:")
    print("-" * 40)
    for workflow in MEDSPA_WORKFLOWS:
        print(f"  {workflow}")
    print()
    print("Available Agents:")
    print("-" * 40)
    agents = sorted(set(TASK_ROUTING.values()))
    for agent in agents:
        keywords = [k for k, v in TASK_ROUTING.items() if v == agent]
        print(f"  {agent}: {', '.join(keywords[:5])}...")
    return 0


def main():
    parser = argparse.ArgumentParser(
        description="MedSpa Development Orchestrator",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=__doc__,
    )

    subparsers = parser.add_subparsers(dest="command", help="Command to run")

    # workflow command
    workflow_parser = subparsers.add_parser("workflow", help="Run a MedSpa workflow")
    workflow_parser.add_argument("name", help="Workflow name")
    workflow_parser.add_argument("-c", "--context", help="JSON context for workflow")
    workflow_parser.set_defaults(func=cmd_workflow)

    # task command
    task_parser = subparsers.add_parser("task", help="Execute a single task")
    task_parser.add_argument("description", help="Task description")
    task_parser.add_argument("-t", "--type", help="Task type (implement, fix, test, etc.)")
    task_parser.add_argument("-a", "--agent", help="Force specific agent")
    task_parser.add_argument("-o", "--output", help="Output directory for artifacts")
    task_parser.set_defaults(func=cmd_task)

    # validate command
    validate_parser = subparsers.add_parser("validate", help="Validate implementations")
    validate_parser.set_defaults(func=cmd_validate)

    # test command
    test_parser = subparsers.add_parser("test", help="Generate tests for component")
    test_parser.add_argument("component", help="Path to component (e.g., internal/payments/refund.go)")
    test_parser.add_argument("-o", "--output", help="Output directory for tests")
    test_parser.set_defaults(func=cmd_test)

    # list command
    list_parser = subparsers.add_parser("list", help="List available workflows and agents")
    list_parser.set_defaults(func=cmd_list)

    args = parser.parse_args()

    if not args.command:
        parser.print_help()
        return 1

    return args.func(args)


if __name__ == "__main__":
    sys.exit(main())
