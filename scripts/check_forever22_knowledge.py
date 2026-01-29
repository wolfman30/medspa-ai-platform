#!/usr/bin/env python3
"""Check knowledge data for Forever 22 Med Spa."""

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

# Forever 22 Med Spa org ID
FOREVER22_ORG_ID = "d0f9d4b4-05d2-40b3-ad4b-ae9a3b5c8599"
API_URL = "https://api-dev.aiwolfsolutions.com"

def get_secrets():
    """Fetch secrets from AWS Secrets Manager."""
    result = subprocess.run(
        ["aws", "secretsmanager", "get-secret-value",
         "--secret-id", "medspa-development-app-secrets",
         "--query", "SecretString", "--output", "text"],
        capture_output=True, text=True
    )
    if result.returncode != 0:
        print(f"Error fetching secrets: {result.stderr}")
        sys.exit(1)
    return json.loads(result.stdout)

def create_jwt(secret: str) -> str:
    """Create JWT token for admin authentication."""
    header = {"alg": "HS256", "typ": "JWT"}
    header_b64 = base64.urlsafe_b64encode(json.dumps(header).encode()).rstrip(b'=').decode()
    now = int(time.time())
    payload = {"sub": "admin", "iat": now, "exp": now + 3600}
    payload_b64 = base64.urlsafe_b64encode(json.dumps(payload).encode()).rstrip(b'=').decode()
    message = f"{header_b64}.{payload_b64}"
    signature = hmac.new(secret.encode(), message.encode(), hashlib.sha256).digest()
    signature_b64 = base64.urlsafe_b64encode(signature).rstrip(b'=').decode()
    return f"{message}.{signature_b64}"

def api_get(url: str, token: str):
    """Make GET request to API."""
    req = urllib.request.Request(url)
    req.add_header("Authorization", f"Bearer {token}")
    ctx = ssl.create_default_context()
    ctx.check_hostname = False
    ctx.verify_mode = ssl.CERT_NONE
    try:
        with urllib.request.urlopen(req, context=ctx, timeout=30) as response:
            return json.loads(response.read().decode())
    except urllib.error.HTTPError as e:
        body = e.read().decode() if e.fp else ""
        print(f"   [ERROR] HTTP {e.code}: {body[:500]}")
        return {"error": f"HTTP {e.code}"}
    except Exception as e:
        print(f"   [ERROR] Error: {e}")
        return {"error": str(e)}

def main():
    print("=" * 70)
    print("Forever 22 Med Spa - Knowledge Data Check")
    print("=" * 70)
    print(f"\nOrg ID: {FOREVER22_ORG_ID}")
    print(f"API URL: {API_URL}\n")

    # Get admin JWT secret
    secrets = get_secrets()
    token = create_jwt(secrets["ADMIN_JWT_SECRET"])

    # Check portal knowledge endpoint
    print("[1] Portal Knowledge API")
    print("    Endpoint: GET /portal/orgs/{orgID}/knowledge")
    knowledge_url = f"{API_URL}/portal/orgs/{FOREVER22_ORG_ID}/knowledge"
    knowledge = api_get(knowledge_url, token)

    if "error" not in knowledge:
        docs = knowledge.get("documents", [])
        print(f"    [OK] Success! Found {len(docs)} knowledge document(s)")
        if docs:
            print("\n    Knowledge Documents:")
            for i, doc in enumerate(docs, 1):
                doc_str = json.dumps(doc, indent=6)
                # Indent each line
                indented = "\n".join(f"      {line}" for line in doc_str.split("\n"))
                print(f"\n    Document {i}:")
                print(indented)
        else:
            print("\n    [WARN] Warning: documents array is empty")
            print("    The API returned successfully but there's no knowledge data.")
    else:
        print(f"    API returned: {json.dumps(knowledge, indent=4)}")

    # Check org info
    print("\n[2] Organization Info")
    print("    Endpoint: GET /admin/clinics")
    clinics_url = f"{API_URL}/admin/clinics"
    clinics = api_get(clinics_url, token)

    if "error" not in clinics:
        forever22 = next((c for c in clinics.get("clinics", []) if c.get("id") == FOREVER22_ORG_ID), None)
        if forever22:
            print("    [OK] Organization found:")
            print(f"       Name: {forever22.get('name', 'N/A')}")
            print(f"       Owner: {forever22.get('owner_email', 'N/A')}")
            print(f"       Phone: {forever22.get('phone', 'N/A')}")
        else:
            print(f"    [WARN] Organization with ID {FOREVER22_ORG_ID} not found")

    # Check Redis directly (if possible via local connection)
    print("\n[3] Redis Check (if local)")
    print("    To check Redis manually, run:")
    print(f"    redis-cli KEYS '*{FOREVER22_ORG_ID}*'")
    print(f"    redis-cli GET 'knowledge:doc:{FOREVER22_ORG_ID}'")

    print("\n" + "=" * 70)
    print("Summary")
    print("=" * 70)
    print("If knowledge documents are empty:")
    print("  1. Data may not have been seeded in Redis")
    print("  2. Check Redis connection in the backend")
    print("  3. Verify knowledge repository is configured correctly")
    print("\nIf portal UI is not showing knowledge:")
    print("  1. Check browser console for API errors")
    print("  2. Verify orgId is being passed correctly to KnowledgeSettings")
    print("  3. Check that user has correct authentication token")
    print("=" * 70)

if __name__ == "__main__":
    main()
