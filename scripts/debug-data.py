#!/usr/bin/env python3
"""Debug what data exists for Forever 22."""

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

ORG_ID = "bb507f20-7fcc-4941-9eac-9ed93b7834ed"
API_URL = "https://api-dev.aiwolfsolutions.com"

def get_secrets():
    result = subprocess.run(
        ["aws", "secretsmanager", "get-secret-value",
         "--secret-id", "medspa-development-app-secrets",
         "--query", "SecretString", "--output", "text"],
        capture_output=True, text=True
    )
    return json.loads(result.stdout)

def create_jwt(secret: str) -> str:
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
        return {"error": f"HTTP {e.code}: {body[:200]}"}
    except Exception as e:
        return {"error": str(e)}

secrets = get_secrets()
token = create_jwt(secrets["ADMIN_JWT_SECRET"])

print("=" * 60)
print("DEBUG: Forever 22 Med Spa Data")
print("=" * 60)

# Check admin leads endpoint
print("\n1. Admin Leads API (/admin/clinics/{orgID}/leads):")
leads = api_get(f"{API_URL}/admin/clinics/{ORG_ID}/leads", token)
print(f"   Response: {json.dumps(leads, indent=2)[:500]}")

# Check portal dashboard
print("\n2. Portal Dashboard (/portal/orgs/{orgID}/dashboard):")
dashboard = api_get(f"{API_URL}/portal/orgs/{ORG_ID}/dashboard", token)
print(f"   Response: {json.dumps(dashboard, indent=2)}")

# Check portal conversations
print("\n3. Portal Conversations (/portal/orgs/{orgID}/conversations):")
convos = api_get(f"{API_URL}/portal/orgs/{ORG_ID}/conversations", token)
print(f"   Response: {json.dumps(convos, indent=2)[:500]}")

# Check portal deposits
print("\n4. Portal Deposits (/portal/orgs/{orgID}/deposits):")
deposits = api_get(f"{API_URL}/portal/orgs/{ORG_ID}/deposits", token)
print(f"   Response: {json.dumps(deposits, indent=2)[:500]}")

# Check clinic stats
print("\n5. Clinic Stats (/admin/clinics/{orgID}/stats):")
stats = api_get(f"{API_URL}/admin/clinics/{ORG_ID}/stats", token)
print(f"   Response: {json.dumps(stats, indent=2)[:500]}")
