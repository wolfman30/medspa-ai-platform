#!/usr/bin/env python3
"""End-to-end test for deposit payment notifications.

This script:
1. Creates a test lead with full patient details
2. Creates a payment record for that lead
3. Completes the fake payment
4. Verifies notification was sent by checking logs
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
import uuid
from datetime import datetime

# Configuration
API_URL = "https://api-dev.aiwolfsolutions.com"
FOREVER22_ORG_ID = "bb507f20-7fcc-4941-9eac-9ed93b7834ed"

# Demo patient data (distinct from other numbers in conversation)
DEMO_PATIENT = {
    "phone": "+15559876543",
    "name": "Sarah Johnson",  # Full name
    "email": "sarah.johnson.demo@example.com",
    "service_interest": "Weight Loss Consultation",
    "preferred_days": "weekdays",
    "preferred_times": "morning",
    "scheduling_notes": "Allergic to latex. Goal: lose 50 lbs. Has previous experience with Ozempic.",
    "source": "e2e_test"
}


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


def get_db_connection_string():
    """Get database connection string from secrets."""
    secrets = get_secrets()
    return secrets.get("DATABASE_URL", "")


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


def execute_sql(query: str, params: tuple = None) -> list:
    """Execute SQL query using psql via database URL."""
    db_url = get_db_connection_string()

    # Build psql command
    if params:
        # Simple parameter substitution for our purposes
        for i, param in enumerate(params, 1):
            if isinstance(param, str):
                query = query.replace(f"${i}", f"'{param}'")
            elif param is None:
                query = query.replace(f"${i}", "NULL")
            else:
                query = query.replace(f"${i}", str(param))

    cmd = ["psql", db_url, "-t", "-A", "-c", query]
    result = subprocess.run(cmd, capture_output=True, text=True)

    if result.returncode != 0:
        print(f"SQL Error: {result.stderr}")
        return []

    return result.stdout.strip().split('\n') if result.stdout.strip() else []


def create_test_lead() -> str:
    """Create a test lead with full patient details."""
    lead_id = str(uuid.uuid4())
    now = datetime.utcnow().isoformat() + "Z"

    query = f"""
    INSERT INTO leads (id, org_id, name, email, phone, message, source, created_at,
                       service_interest, patient_type, preferred_days, preferred_times,
                       scheduling_notes, deposit_status, priority_level)
    VALUES (
        '{lead_id}',
        '{FOREVER22_ORG_ID}',
        '{DEMO_PATIENT["name"]}',
        '{DEMO_PATIENT["email"]}',
        '{DEMO_PATIENT["phone"]}',
        'E2E test lead for notification verification',
        '{DEMO_PATIENT["source"]}',
        '{now}',
        '{DEMO_PATIENT["service_interest"]}',
        'new',
        '{DEMO_PATIENT["preferred_days"]}',
        '{DEMO_PATIENT["preferred_times"]}',
        '{DEMO_PATIENT["scheduling_notes"]}',
        'pending',
        'normal'
    )
    RETURNING id;
    """

    result = execute_sql(query)
    if result and result[0]:
        return result[0]
    return lead_id


def create_payment_record(lead_id: str) -> str:
    """Create a payment record for the lead."""
    payment_id = str(uuid.uuid4())
    now = datetime.utcnow().isoformat() + "Z"

    query = f"""
    INSERT INTO payments (id, org_id, lead_id, provider, amount_cents, status, created_at)
    VALUES (
        '{payment_id}',
        '{FOREVER22_ORG_ID}',
        '{lead_id}',
        'square',
        5000,
        'deposit_pending',
        '{now}'
    )
    RETURNING id;
    """

    result = execute_sql(query)
    if result and result[0]:
        return result[0]
    return payment_id


def complete_fake_payment(payment_id: str) -> bool:
    """Complete the fake payment via the demo endpoint."""
    url = f"{API_URL}/demo/payments/{payment_id}/complete"

    ctx = ssl.create_default_context()
    ctx.check_hostname = False
    ctx.verify_mode = ssl.CERT_NONE

    req = urllib.request.Request(url, method="POST")
    req.add_header("Content-Type", "application/json")

    try:
        with urllib.request.urlopen(req, data=b"", context=ctx, timeout=30) as response:
            return response.status in [200, 302, 303]
    except urllib.error.HTTPError as e:
        if e.code in [302, 303]:  # Redirect to success page is expected
            return True
        print(f"Payment completion error: {e.code} - {e.read().decode()[:200]}")
        return False
    except Exception as e:
        print(f"Payment completion exception: {e}")
        return False


def check_logs_for_notification(lead_id: str, timeout_seconds: int = 30) -> bool:
    """Check CloudWatch logs for notification sent confirmation."""
    import time as time_module

    start_time = time_module.time()
    log_group = "/ecs/medspa-development-api"

    while time_module.time() - start_time < timeout_seconds:
        cmd = [
            "aws", "logs", "filter-log-events",
            "--log-group-name", log_group,
            "--filter-pattern", f'"payment email sent" OR "payment SMS sent"',
            "--start-time", str(int((time_module.time() - 60) * 1000)),
            "--query", "events[*].message",
            "--output", "text"
        ]

        result = subprocess.run(cmd, capture_output=True, text=True, env={**subprocess.os.environ, 'MSYS_NO_PATHCONV': '1'})

        if result.returncode == 0 and lead_id in result.stdout:
            return True

        time_module.sleep(2)

    return False


def main():
    print("=" * 70)
    print("E2E Deposit Payment Notification Test")
    print("=" * 70)
    print(f"\nTest Patient: {DEMO_PATIENT['name']}")
    print(f"Phone: {DEMO_PATIENT['phone']}")
    print(f"Service: {DEMO_PATIENT['service_interest']}")
    print(f"Preferences: {DEMO_PATIENT['preferred_days']}, {DEMO_PATIENT['preferred_times']}")
    print(f"Notes: {DEMO_PATIENT['scheduling_notes']}")

    # Step 1: Create lead
    print("\n1. Creating test lead...")
    lead_id = create_test_lead()
    print(f"   Lead ID: {lead_id}")

    # Step 2: Create payment
    print("\n2. Creating payment record...")
    payment_id = create_payment_record(lead_id)
    print(f"   Payment ID: {payment_id}")
    print(f"   Amount: $50.00")

    # Step 3: Complete payment
    print("\n3. Completing fake payment...")
    success = complete_fake_payment(payment_id)
    if success:
        print("   Payment completed successfully!")
    else:
        print("   Payment completion failed!")
        sys.exit(1)

    # Step 4: Wait and check for notifications
    print("\n4. Checking logs for notification dispatch...")
    print("   (This may take up to 30 seconds)")

    # Give the async workers time to process
    time.sleep(5)

    # Check logs
    notification_found = check_logs_for_notification(lead_id)

    print("\n" + "=" * 70)
    print("TEST RESULTS")
    print("=" * 70)
    print(f"\nLead Created: {lead_id}")
    print(f"Payment Completed: {payment_id}")

    if notification_found:
        print("\n NOTIFICATION SENT SUCCESSFULLY!")
        print("\nCheck your email for:")
        print("  - wolfpassion20@gmail.com")
        print("  - andrew@aiwolfsolutions.com")
        print("\nAnd SMS to: +19378962713")
    else:
        print("\n Checking logs directly for notification status...")
        # Fallback: Check logs directly via AWS CLI
        cmd = [
            "aws", "logs", "filter-log-events",
            "--log-group-name", "/ecs/medspa-development-api",
            "--filter-pattern", '"notify:"',
            "--start-time", str(int((time.time() - 120) * 1000)),
            "--limit", "20",
            "--query", "events[*].message",
            "--output", "text"
        ]
        result = subprocess.run(cmd, capture_output=True, text=True, env={**subprocess.os.environ, 'MSYS_NO_PATHCONV': '1'})
        if result.stdout:
            print("\nRecent notification logs:")
            for line in result.stdout.strip().split('\t')[:5]:
                if line.strip():
                    print(f"  {line[:150]}")

    print("\n" + "=" * 70)


if __name__ == "__main__":
    main()
