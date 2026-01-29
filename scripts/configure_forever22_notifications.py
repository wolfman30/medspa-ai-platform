#!/usr/bin/env python3
"""Configure notification settings for Forever 22 Med Spa.

This script sets up email and SMS notifications for deposit payments.
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

# Configuration
API_URL = "https://api-dev.aiwolfsolutions.com"
FOREVER22_ORG_ID = "bb507f20-7fcc-4941-9eac-9ed93b7834ed"

# Notification recipients
EMAIL_RECIPIENTS = ["wolfpassion20@gmail.com", "andrew@aiwolfsolutions.com"]
SMS_RECIPIENTS = ["+19378962713"]  # Format: +1XXXXXXXXXX


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
    """Create a JWT for admin API access."""
    header = {"alg": "HS256", "typ": "JWT"}
    header_b64 = base64.urlsafe_b64encode(json.dumps(header).encode()).rstrip(b'=').decode()
    now = int(time.time())
    payload = {"sub": "admin", "iat": now, "exp": now + 3600}
    payload_b64 = base64.urlsafe_b64encode(json.dumps(payload).encode()).rstrip(b'=').decode()
    message = f"{header_b64}.{payload_b64}"
    signature = hmac.new(secret.encode(), message.encode(), hashlib.sha256).digest()
    signature_b64 = base64.urlsafe_b64encode(signature).rstrip(b'=').decode()
    return f"{message}.{signature_b64}"


def api_request(url: str, token: str, method: str = "GET", data: dict = None):
    """Make an API request."""
    req = urllib.request.Request(url, method=method)
    req.add_header("Authorization", f"Bearer {token}")
    req.add_header("Content-Type", "application/json")

    ctx = ssl.create_default_context()
    ctx.check_hostname = False
    ctx.verify_mode = ssl.CERT_NONE

    body = None
    if data:
        body = json.dumps(data).encode()

    try:
        with urllib.request.urlopen(req, data=body, context=ctx, timeout=30) as response:
            return json.loads(response.read().decode()), response.status
    except urllib.error.HTTPError as e:
        error_body = e.read().decode() if e.fp else ""
        return {"error": f"HTTP {e.code}: {error_body[:500]}"}, e.code
    except Exception as e:
        return {"error": str(e)}, 0


def main():
    print("=" * 60)
    print("Forever 22 Med Spa - Notification Settings Configuration")
    print("=" * 60)

    # Get secrets and create JWT
    print("\n1. Authenticating...")
    secrets = get_secrets()
    token = create_jwt(secrets["ADMIN_JWT_SECRET"])
    print("   Authentication successful")

    # Get current notification settings
    print(f"\n2. Fetching current notification settings for org {FOREVER22_ORG_ID}...")
    url = f"{API_URL}/admin/orgs/{FOREVER22_ORG_ID}/notifications"
    current_settings, status = api_request(url, token)

    if status != 200:
        print(f"   Error: {current_settings}")
        sys.exit(1)

    print("   Current settings:")
    print(f"     Email Enabled: {current_settings.get('email_enabled', False)}")
    print(f"     Email Recipients: {current_settings.get('email_recipients', [])}")
    print(f"     SMS Enabled: {current_settings.get('sms_enabled', False)}")
    print(f"     SMS Recipients: {current_settings.get('sms_recipients', [])}")
    print(f"     Notify On Payment: {current_settings.get('notify_on_payment', False)}")
    print(f"     Notify On New Lead: {current_settings.get('notify_on_new_lead', False)}")

    # Update notification settings
    print("\n3. Updating notification settings...")
    new_settings = {
        "email_enabled": True,
        "email_recipients": EMAIL_RECIPIENTS,
        "sms_enabled": True,
        "sms_recipients": SMS_RECIPIENTS,
        "notify_on_payment": True,
        "notify_on_new_lead": True  # Also enable new lead notifications
    }

    print(f"   New settings to apply:")
    print(f"     Email Enabled: {new_settings['email_enabled']}")
    print(f"     Email Recipients: {new_settings['email_recipients']}")
    print(f"     SMS Enabled: {new_settings['sms_enabled']}")
    print(f"     SMS Recipients: {new_settings['sms_recipients']}")
    print(f"     Notify On Payment: {new_settings['notify_on_payment']}")
    print(f"     Notify On New Lead: {new_settings['notify_on_new_lead']}")

    updated_settings, status = api_request(url, token, method="PUT", data=new_settings)

    if status != 200:
        print(f"   Error updating settings: {updated_settings}")
        sys.exit(1)

    print("\n4. Settings updated successfully!")
    print("   Updated settings:")
    print(f"     Email Enabled: {updated_settings.get('email_enabled', False)}")
    print(f"     Email Recipients: {updated_settings.get('email_recipients', [])}")
    print(f"     SMS Enabled: {updated_settings.get('sms_enabled', False)}")
    print(f"     SMS Recipients: {updated_settings.get('sms_recipients', [])}")
    print(f"     Notify On Payment: {updated_settings.get('notify_on_payment', False)}")
    print(f"     Notify On New Lead: {updated_settings.get('notify_on_new_lead', False)}")

    print("\n" + "=" * 60)
    print("DONE! Forever 22 will now receive notifications when:")
    print("  - A deposit payment is completed (email + SMS)")
    print("  - A new lead comes in (email + SMS)")
    print("=" * 60)


if __name__ == "__main__":
    main()
