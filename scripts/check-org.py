#!/usr/bin/env python3
"""Check org ID for clinic@example.com"""

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

API_URL = "https://api-dev.aiwolfsolutions.com"

def get_secrets():
    result = subprocess.run(
        ["aws", "secretsmanager", "get-secret-value",
         "--secret-id", "medspa-development-app-secrets",
         "--query", "SecretString", "--output", "text"],
        capture_output=True, text=True
    )
    return json.loads(result.stdout)

def create_jwt(secret: str, email: str = None) -> str:
    header = {"alg": "HS256", "typ": "JWT"}
    header_b64 = base64.urlsafe_b64encode(json.dumps(header).encode()).rstrip(b'=').decode()
    now = int(time.time())
    payload = {"sub": "admin", "iat": now, "exp": now + 3600}
    if email:
        payload["email"] = email
    payload_b64 = base64.urlsafe_b64encode(json.dumps(payload).encode()).rstrip(b'=').decode()
    message = f"{header_b64}.{payload_b64}"
    signature = hmac.new(secret.encode(), message.encode(), hashlib.sha256).digest()
    signature_b64 = base64.urlsafe_b64encode(signature).rstrip(b'=').decode()
    return f"{message}.{signature_b64}"

def api_get(url: str, token: str):
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
        return {"error": f"HTTP {e.code}: {body[:500]}"}
    except Exception as e:
        return {"error": str(e)}

secrets = get_secrets()
token = create_jwt(secrets["ADMIN_JWT_SECRET"])

print("=" * 60)
print("Checking org mappings")
print("=" * 60)

# Check all orgs/clinics
print("\n1. List all clinics:")
clinics = api_get(f"{API_URL}/admin/clinics", token)
print(f"   Response: {json.dumps(clinics, indent=2)}")

# Check org lookup by email
print("\n2. Looking up org by email (clinic@example.com):")
org_lookup = api_get(f"{API_URL}/api/client/org?email=clinic@example.com", token)
print(f"   Response: {json.dumps(org_lookup, indent=2)}")

# Check the known Forever 22 org ID
FOREVER22_ORG = "bb507f20-7fcc-4941-9eac-9ed93b7834ed"
print(f"\n3. Direct check of Forever 22 org ({FOREVER22_ORG}):")
dashboard = api_get(f"{API_URL}/portal/orgs/{FOREVER22_ORG}/dashboard", token)
print(f"   Dashboard: {json.dumps(dashboard, indent=2)}")

# Check if there are multiple orgs
print("\n4. Check organizations table directly via admin endpoint:")
admin_dashboard = api_get(f"{API_URL}/admin/dashboard", token)
print(f"   Admin dashboard: {json.dumps(admin_dashboard, indent=2)[:1000]}")
