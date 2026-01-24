#!/usr/bin/env python3
"""
Purge phone data for a clinic.

Usage:
    python scripts/purge.py                     # Purge all phones for Forever 22
    python scripts/purge.py 9378962713          # Purge specific phone for Forever 22
    python scripts/purge.py --org OTHER_ORG_ID  # Purge all phones for different org
    python scripts/purge.py --list              # List all phones for Forever 22
"""

import json
import subprocess
import sys
import time
import hmac
import hashlib
import base64
import urllib.request
import urllib.error
import ssl
import argparse

# Default org: Forever 22 Med Spa
DEFAULT_ORG_ID = "bb507f20-7fcc-4941-9eac-9ed93b7834ed"
API_URL = "https://api-dev.aiwolfsolutions.com"

def get_secrets():
    """Get secrets from AWS Secrets Manager."""
    result = subprocess.run(
        ["aws", "secretsmanager", "get-secret-value",
         "--secret-id", "medspa-development-app-secrets",
         "--query", "SecretString",
         "--output", "text"],
        capture_output=True, text=True
    )
    if result.returncode != 0:
        print(f"Failed to get secret: {result.stderr}")
        sys.exit(1)
    return json.loads(result.stdout)

def create_jwt(secret: str) -> str:
    """Create an HS256 JWT token."""
    header = {"alg": "HS256", "typ": "JWT"}
    header_b64 = base64.urlsafe_b64encode(json.dumps(header).encode()).rstrip(b'=').decode()
    now = int(time.time())
    payload = {"sub": "admin", "iat": now, "exp": now + 3600}
    payload_b64 = base64.urlsafe_b64encode(json.dumps(payload).encode()).rstrip(b'=').decode()
    message = f"{header_b64}.{payload_b64}"
    signature = hmac.new(secret.encode(), message.encode(), hashlib.sha256).digest()
    signature_b64 = base64.urlsafe_b64encode(signature).rstrip(b'=').decode()
    return f"{message}.{signature_b64}"

def api_request(url: str, token: str, method: str = "GET"):
    """Make an API request."""
    req = urllib.request.Request(url, method=method)
    req.add_header("Authorization", f"Bearer {token}")
    req.add_header("Content-Type", "application/json")

    ctx = ssl.create_default_context()
    ctx.check_hostname = False
    ctx.verify_mode = ssl.CERT_NONE

    try:
        with urllib.request.urlopen(req, context=ctx, timeout=30) as response:
            return json.loads(response.read().decode())
    except urllib.error.HTTPError as e:
        body = e.read().decode() if e.fp else ""
        print(f"HTTP Error {e.code}: {e.reason} - {body}")
        return None
    except urllib.error.URLError as e:
        print(f"URL Error: {e.reason}")
        return None

def get_leads(org_id: str, token: str):
    """Get all leads for an org."""
    url = f"{API_URL}/admin/clinics/{org_id}/leads"
    return api_request(url, token)

def purge_phone(org_id: str, phone: str, token: str):
    """Purge a phone number."""
    # Normalize phone to digits only
    digits = ''.join(c for c in phone if c.isdigit())
    if len(digits) == 11 and digits.startswith('1'):
        digits = digits[1:]  # Remove leading 1 for API

    url = f"{API_URL}/admin/clinics/{org_id}/phones/{digits}"
    print(f"  Purging {phone} ({digits})...", end=" ")

    result = api_request(url, token, method="DELETE")
    if result:
        deleted = result.get("deleted", {})
        total = sum(deleted.values())
        print(f"OK ({total} records)")
        return result
    else:
        print("FAILED")
        return None

def purge_all(org_id: str, token: str):
    """Purge ALL data for an org."""
    url = f"{API_URL}/admin/clinics/{org_id}/data"
    print(f"Purging ALL data for org {org_id}...")

    result = api_request(url, token, method="DELETE")
    if result:
        deleted = result.get("deleted", {})
        print("\nDeleted:")
        for k, v in deleted.items():
            if v > 0:
                print(f"  {k}: {v}")
        return result
    else:
        print("FAILED")
        return None

def main():
    parser = argparse.ArgumentParser(description="Purge data for Forever 22 Med Spa")
    parser.add_argument("phone", nargs="?", help="Specific phone number to purge (optional)")
    parser.add_argument("--org", default=DEFAULT_ORG_ID, help="Org ID (default: Forever 22)")
    parser.add_argument("--list", action="store_true", help="List phones without purging")
    parser.add_argument("--by-phone", action="store_true", help="Purge by phone numbers instead of all data")
    args = parser.parse_args()

    print("Getting credentials...")
    secrets = get_secrets()
    token = create_jwt(secrets["ADMIN_JWT_SECRET"])

    org_id = args.org
    org_name = "Forever 22 Med Spa" if org_id == DEFAULT_ORG_ID else org_id

    if args.phone:
        # Purge specific phone
        print(f"\nPurging phone {args.phone} for {org_name}...")
        result = purge_phone(org_id, args.phone, token)
        if result:
            print("\nDeleted:")
            for k, v in result.get("deleted", {}).items():
                if v > 0:
                    print(f"  {k}: {v}")
    elif args.list:
        # Just list phones
        print(f"\nGetting leads for {org_name}...")
        leads_response = get_leads(org_id, token)
        if leads_response:
            leads = leads_response.get("leads", [])
            phones = set()
            for lead in leads:
                phone = lead.get("phone", "")
                if phone:
                    digits = ''.join(c for c in phone if c.isdigit())
                    if digits:
                        phones.add(digits)
            print(f"Found {len(phones)} unique phone number(s):")
            for p in sorted(phones):
                formatted = f"({p[:3]}) {p[3:6]}-{p[6:]}" if len(p) == 10 else p
                print(f"  {formatted}")
    elif args.by_phone:
        # Purge by iterating through phones (old behavior)
        print(f"\nGetting leads for {org_name}...")
        leads_response = get_leads(org_id, token)

        if not leads_response:
            print("Could not get leads")
            sys.exit(1)

        leads = leads_response.get("leads", [])
        if not leads:
            print("No leads found")
            return

        phones = set()
        for lead in leads:
            phone = lead.get("phone", "")
            if phone:
                digits = ''.join(c for c in phone if c.isdigit())
                if digits:
                    phones.add(digits)

        print(f"Found {len(phones)} unique phone number(s)")
        print(f"Purging all {len(phones)} phone number(s)...")
        total_deleted = {}
        for phone in phones:
            result = purge_phone(org_id, phone, token)
            if result:
                for k, v in result.get("deleted", {}).items():
                    total_deleted[k] = total_deleted.get(k, 0) + v

        print("\n" + "="*40)
        print("TOTAL DELETED:")
        for k, v in total_deleted.items():
            if v > 0:
                print(f"  {k}: {v}")
        print("="*40)
    else:
        # Default: Purge ALL data for the org
        print(f"\n{'='*50}")
        print(f"PURGING ALL DATA FOR {org_name.upper()}")
        print(f"{'='*50}")
        purge_all(org_id, token)

if __name__ == "__main__":
    main()
