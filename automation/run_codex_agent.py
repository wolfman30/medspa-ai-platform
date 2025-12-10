#!/usr/bin/env python3
"""
run_codex_agent.py
-------------------

This script orchestrates OpenAI's GPT-5.1-Codex-Max as an autonomous agent
to incrementally evolve the `medspa-ai-platform` codebase toward its
Revenue MVP target state.  It is designed to run periodically (e.g., every
30 minutes for a 30-minute slot) and will:

1.  Pull or clone the latest version of the `medspa-ai-platform` repository
    from GitHub.
2.  Read the Revenue MVP specification from `docs/revenue-mvp.md` within
    that repository (this file defines the desired target state).
3.  Spawn a Codex agent process with a 30-minute execution limit that
    attempts to bridge the gap between the current codebase and the target
    specification.  The agent should create a new feature branch for
    its changes and leave behind commits for human review.

**Important:**  This script requires:
- Git (e.g., via `apt install git`)
- Python dependencies (`gitpython`, `schedule`)
- OpenAI Codex CLI (install via: npm i -g @openai/codex)
- OPENAI_API_KEY environment variable set

"""

from __future__ import annotations

import argparse
import datetime
import os
import subprocess
import time
from pathlib import Path
import shutil

import schedule  # pip install schedule

try:
    import git  # pip install gitpython
except ImportError as exc:
    raise ImportError(
        "The gitpython library is required.  Install it with 'pip install gitpython'"
    ) from exc


def sync_repository(repo_url: str, local_path: Path) -> None:
    """Clone the repository if it does not exist locally; otherwise fetch
    and reset to the remote HEAD.  This ensures that the Codex agent
    always operates on a clean, up-to-date copy of your codebase.

    Args:
        repo_url: The SSH or HTTPS URL of the GitHub repository.
        local_path: The directory where the repository should live.
    """
    if not local_path.exists():
        print(f"Cloning repository {repo_url} into {local_path} ...")
        git.Repo.clone_from(repo_url, local_path)
    else:
        print(f"Updating repository at {local_path} ...")
        repo = git.Repo(local_path)
        # Discard any local changes and fetch the latest from origin/main
        repo.git.fetch('origin')
        repo.git.reset('--hard', 'origin/main')


def run_codex_task(repo_path: Path) -> None:
    """Invoke the Codex agent on the repository with a 30-minute limit.

    This function constructs a unique branch name based on the current
    timestamp, then calls the Codex CLI (`codex exec ...`) to run the
    agent.  Adjust the arguments to suit your workflow.

    Args:
        repo_path: Path to the local checkout of the repository.
    """
    # Derive a branch name that encodes the time when the agent runs.
    ts = datetime.datetime.now(datetime.UTC).strftime("%Y%m%d%H%M")
    branch_name = f"codex-auto-{ts}"

    # Construct the path to the Revenue MVP specification.
    target_spec = repo_path / 'docs' / 'revenue-mvp.md'
    if not target_spec.exists():
        raise FileNotFoundError(f"Specification file not found: {target_spec}")

    # Read the task specification to use as the prompt
    task_content = target_spec.read_text(encoding='utf-8')

    # Create and checkout the feature branch before running Codex
    repo = git.Repo(repo_path)
    repo.git.checkout('-b', branch_name)
    print(f"Created and checked out branch: {branch_name}")

    # Resolve Codex binary (allow override via CODEX_BIN env)
    codex_bin = os.environ.get('CODEX_BIN', 'codex')
    resolved = shutil.which(codex_bin)
    if resolved is None:
        raise FileNotFoundError(
            f"Codex CLI not found (tried '{codex_bin}'). "
            "Install via: npm i -g @openai/codex"
        )

    # Assemble the Codex CLI command using the exec subcommand.
    # --model: selects GPT-5.1-Codex-Max
    # --full-auto: enables autonomous execution with workspace write access
    # -C: sets the working directory
    prompt = f"Implement the following specification. Make incremental commits as you work:\n\n{task_content}"
    cmd = [
        resolved,
        'exec',
        '--model', 'gpt-5.1-codex-max',
        '--full-auto',
        '-C', str(repo_path),
        prompt
    ]
    print(f"Running Codex agent: {resolved} exec --model gpt-5.1-codex-max --full-auto -C {repo_path} <prompt>")
    try:
        subprocess.run(cmd, check=True, timeout=1800)
    except subprocess.TimeoutExpired:
        print("Codex agent reached 30-minute timeout limit")
    except subprocess.CalledProcessError as exc:
        print(f"Codex agent failed with exit code {exc.returncode}: {exc}")


def periodic_job() -> None:
    """The top-level job that runs on a schedule.

    When called, this function synchronizes the local repository and then
    launches the Codex agent.  If anything goes wrong during the
    synchronization step (e.g., network issues) the exception will
    propagate and prevent the agent from running.
    """
    # Repository configuration. Prefer HTTPS and the existing local checkout.
    # These can still be overridden via environment variables.
    repo_url = os.environ.get('REPO_URL', 'https://github.com/wolfman30/medspa-ai-platform.git')
    repo_path = Path(os.environ.get('REPO_PATH', r'C:\Users\wolfp\ai-agency\medspa-ai-platform'))

    sync_repository(repo_url, repo_path)

    # Before launching the agent, check for an existing auto-generated
    # feature branch on the remote.  If a branch starting with
    # ``codex-auto-`` exists on ``origin``, we treat it as a pending pull
    # request and skip this run.
    repo = git.Repo(repo_path)
    try:
        repo.remotes.origin.fetch()
    except Exception as e:
        print(f"Failed to fetch remote branches: {e}")
    pending = False
    for ref in repo.remotes.origin.refs:
        head = getattr(ref, 'remote_head', '')
        if head.startswith('codex-auto-'):
            pending = True
            break
    if pending:
        print(
            "Pending auto-generated branch detected. Skipping this run until the PR is reviewed and merged."
        )
        return

    run_codex_task(repo_path)


def main() -> None:
    """Entry point to schedule the periodic job.

    This sets up a recurring schedule that triggers the job every
    30 minutes.  It then enters an infinite loop, invoking any
    pending tasks at the appropriate times.  Adjust the schedule if
    you need different timing.
    """
    parser = argparse.ArgumentParser(description='Run Codex agent scheduler')
    parser.add_argument('--config', type=str, help='Path to config file (currently unused)')
    parser.add_argument('--once', action='store_true', help='Run once immediately and exit')
    args = parser.parse_args()

    print("Starting the Codex agent scheduler ...")

    if args.once:
        # Run once and exit
        periodic_job()
        return

    # Schedule the job at startup and then every 30 minutes thereafter.
    schedule.every().minute.at(':00').do(periodic_job)  # immediate run at program start
    schedule.every(30).minutes.do(periodic_job)

    while True:
        schedule.run_pending()
        time.sleep(10)


if __name__ == '__main__':
    main()
