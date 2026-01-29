#!/usr/bin/env python3
"""Verify email addresses in AWS SES for sandbox mode.

In SES sandbox, both sender AND recipient emails must be verified.
This script verifies the notification recipient emails.
"""

import subprocess
import sys
import json

# Emails to verify (recipients for deposit notifications)
EMAILS_TO_VERIFY = [
    "wolfpassion20@gmail.com",
    "andrew@aiwolfsolutions.com",
]

# AWS region for SES
AWS_REGION = "us-east-1"


def run_aws_command(args: list) -> tuple[bool, str]:
    """Run an AWS CLI command and return success status and output."""
    result = subprocess.run(
        ["aws"] + args,
        capture_output=True,
        text=True
    )
    if result.returncode != 0:
        return False, result.stderr
    return True, result.stdout


def get_verified_emails() -> list[str]:
    """Get list of already verified email addresses."""
    success, output = run_aws_command([
        "sesv2", "list-email-identities",
        "--region", AWS_REGION,
        "--output", "json"
    ])
    if not success:
        print(f"Warning: Could not list verified emails: {output}")
        return []

    try:
        data = json.loads(output)
        return [
            identity["IdentityName"]
            for identity in data.get("EmailIdentities", [])
            if identity.get("IdentityType") == "EMAIL_ADDRESS"
        ]
    except json.JSONDecodeError:
        return []


def verify_email(email: str) -> bool:
    """Send verification email to an address."""
    success, output = run_aws_command([
        "sesv2", "create-email-identity",
        "--email-identity", email,
        "--region", AWS_REGION
    ])
    if not success:
        if "AlreadyExistsException" in output:
            return True  # Already exists, that's fine
        print(f"   Error: {output}")
        return False
    return True


def check_verification_status(email: str) -> str:
    """Check if an email is verified."""
    success, output = run_aws_command([
        "sesv2", "get-email-identity",
        "--email-identity", email,
        "--region", AWS_REGION,
        "--output", "json"
    ])
    if not success:
        return "NOT_FOUND"

    try:
        data = json.loads(output)
        if data.get("VerifiedForSendingStatus", False):
            return "VERIFIED"
        return "PENDING"
    except json.JSONDecodeError:
        return "UNKNOWN"


def main():
    print("=" * 60)
    print("AWS SES Email Verification Setup")
    print("=" * 60)
    print(f"\nRegion: {AWS_REGION}")
    print(f"Emails to verify: {', '.join(EMAILS_TO_VERIFY)}")

    # Check current verification status
    print("\n1. Checking current verification status...")
    verified_emails = get_verified_emails()
    print(f"   Currently verified emails: {verified_emails if verified_emails else 'None'}")

    # Process each email
    print("\n2. Processing emails...")
    results = []

    for email in EMAILS_TO_VERIFY:
        print(f"\n   Processing: {email}")

        # Check current status
        status = check_verification_status(email)

        if status == "VERIFIED":
            print(f"   ✓ Already verified")
            results.append((email, "VERIFIED"))
            continue

        if status == "PENDING":
            print(f"   ⏳ Verification pending - check inbox for verification email")
            results.append((email, "PENDING"))
            continue

        # Need to create verification request
        print(f"   Sending verification email...")
        if verify_email(email):
            print(f"   ✓ Verification email sent - check inbox and click the link!")
            results.append((email, "PENDING"))
        else:
            print(f"   ✗ Failed to send verification")
            results.append((email, "FAILED"))

    # Summary
    print("\n" + "=" * 60)
    print("SUMMARY")
    print("=" * 60)

    all_verified = True
    for email, status in results:
        icon = "✓" if status == "VERIFIED" else "⏳" if status == "PENDING" else "✗"
        print(f"  {icon} {email}: {status}")
        if status != "VERIFIED":
            all_verified = False

    if not all_verified:
        print("\n⚠️  ACTION REQUIRED:")
        print("   Check the inbox of each PENDING email and click the verification link.")
        print("   Once verified, deposit notifications will be sent to these addresses.")
    else:
        print("\n✓ All emails are verified! Deposit notifications are ready to send.")

    print("\nTo check status later, run this script again.")
    print("=" * 60)


if __name__ == "__main__":
    main()
