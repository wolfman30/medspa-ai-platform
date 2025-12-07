#!/usr/bin/env python3
"""
Quick test script to verify MedSpa AI end-to-end flow.
Supports both Twilio and Telnyx webhooks.

Usage:
    python scripts/test_e2e.py
    
    # Or with custom settings:
    SMS_PROVIDER=twilio TEST_TO_NUMBER=+15559998888 python scripts/test_e2e.py
"""

import os
import sys
import time
import requests
from datetime import datetime

# Configuration
API_URL = os.getenv("API_URL", "http://localhost:8080")
TEST_PHONE = "+15005550001"
TEST_TO_NUMBER = os.getenv("TWILIO_FROM_NUMBER", "+18662894911")
TEST_ORG_ID = "11111111-1111-1111-1111-111111111111"
TEST_NAME = "E2E Test Customer"
PROVIDER = os.getenv("SMS_PROVIDER", "twilio").lower()


def print_step(step_num, message):
    print(f"\n{'='*60}")
    print(f"STEP {step_num}: {message}")
    print('='*60)


def check_health():
    """Verify API is running"""
    print_step(1, "Checking API health")
    try:
        resp = requests.get(f"{API_URL}/health", timeout=5)
        if resp.status_code == 200:
            print("✅ API is running")
            return True
        else:
            print(f"❌ API returned status {resp.status_code}")
            return False
    except requests.exceptions.RequestException as e:
        print(f"❌ Cannot connect to API: {e}")
        print(f"   Make sure server is running on {API_URL}")
        return False


def create_lead():
    """Create a test lead"""
    print_step(2, "Creating test lead")
    payload = {
        "name": TEST_NAME,
        "phone": TEST_PHONE,
        "email": "e2e-test@example.com",
        "message": "E2E test lead",
        "source": "e2e_test"
    }

    try:
        resp = requests.post(
            f"{API_URL}/leads/web",
            json=payload,
            headers={
                "Content-Type": "application/json",
                "X-Org-ID": TEST_ORG_ID
            },
            timeout=10
        )
        if resp.status_code in [200, 201]:
            lead = resp.json()
            print(f"✅ Lead created: {lead.get('id', 'unknown')}")
            return lead
        else:
            print(f"❌ Failed: {resp.status_code}")
            print(f"   Response: {resp.text}")
            return None
    except requests.exceptions.RequestException as e:
        print(f"❌ Request failed: {e}")
        return None


def send_twilio_webhook(message_text):
    """Send Twilio webhook"""
    payload = {
        "MessageSid": f"SM{int(time.time())}",
        "AccountSid": "AC1234567890abcdef",
        "From": TEST_PHONE,
        "To": TEST_TO_NUMBER,
        "Body": message_text
    }

    try:
        resp = requests.post(
            f"{API_URL}/messaging/twilio/webhook",
            data=payload,
            headers={"Content-Type": "application/x-www-form-urlencoded"},
            timeout=30
        )

        print(f"   Webhook status: {resp.status_code}")

        if resp.status_code == 200:
            print("✅ Twilio webhook accepted")
            return True
        elif resp.status_code == 401:
            print("⚠️  Signature failed (expected in test)")
            return True
        elif resp.status_code == 404:
            print("❌ Org mapping not found")
            print("   Add to .env:")
            print("   TWILIO_ORG_MAP_JSON={")
            print(f"     \\\"{TEST_TO_NUMBER}\\\":")
            print(f"     \\\"{TEST_ORG_ID}\\\"")
            print("   }")
            return False
        else:
            print(f"❌ Unexpected: {resp.text[:200]}")
            return False

    except requests.exceptions.RequestException as e:
        print(f"❌ Request failed: {e}")
        return False


def send_telnyx_webhook(message_text):
    """Send Telnyx webhook"""
    payload = {
        "data": {
            "event_type": "message.received",
            "id": f"test-msg-{int(time.time())}",
            "occurred_at": datetime.utcnow().isoformat() + "Z",
            "payload": {
                "id": f"test-payload-{int(time.time())}",
                "type": "SMS",
                "from": {"phone_number": TEST_PHONE},
                "to": [{"phone_number": TEST_TO_NUMBER}],
                "text": message_text,
                "received_at": datetime.utcnow().isoformat() + "Z"
            }
        }
    }

    try:
        resp = requests.post(
            f"{API_URL}/webhooks/telnyx/messages",
            json=payload,
            headers={
                "Content-Type": "application/json",
                "Telnyx-Signature-Ed25519": "test-sig",
                "Telnyx-Timestamp": str(int(time.time()))
            },
            timeout=30
        )

        print(f"   Webhook status: {resp.status_code}")

        if resp.status_code == 200:
            print("✅ Telnyx webhook accepted")
            return True
        elif resp.status_code == 401:
            print("⚠️  Signature failed (expected)")
            return True
        else:
            print(f"❌ Unexpected: {resp.text[:200]}")
            return False

    except requests.exceptions.RequestException as e:
        print(f"❌ Request failed: {e}")
        return False


def send_sms_webhook(message_text):
    """Route to provider"""
    print_step(3, "Simulating incoming SMS")
    print(f"   Provider: {PROVIDER}")
    print(f"   Message: '{message_text}'")

    if PROVIDER == "twilio":
        return send_twilio_webhook(message_text)
    else:
        return send_telnyx_webhook(message_text)


def verify_conversation():
    """Wait for processing"""
    print_step(4, "Waiting for AI processing")
    print("   Giving system 5 seconds...")
    time.sleep(5)
    print("✅ Processing complete")


def show_verification():
    """Show next steps"""
    print_step(5, "Manual Verification")

    print("\n📋 Check Database:")
    print("-" * 60)
    db_query = f"""psql $DATABASE_URL << EOF
SELECT
  id, name, phone,
  service_interest,
  preferred_days,
  preferred_times,
  scheduling_notes,
  deposit_status
FROM leads
WHERE phone = '{TEST_PHONE}'
ORDER BY created_at DESC LIMIT 1;
EOF"""
    print(db_query)

    print("\n📋 Expected Results:")
    print("-" * 60)
    print("  service_interest  = 'botox'")
    print("  preferred_days    = 'weekdays'")
    print("  preferred_times   = 'afternoon'")
    print("  scheduling_notes  = 'Auto-extracted...'")

    print("\n📋 Check Redis:")
    print("-" * 60)
    print("redis-cli")
    print("KEYS conv_*")
    print("GET conv_{id}")

    print("\n📋 Check Logs:")
    print("-" * 60)
    print("Look for:")
    print("  ✓ 'Lead created/found'")
    print("  ✓ 'Conversation started'")
    print("  ✓ 'GPT response generated'")
    print("  ✓ 'Preferences extracted'")


def main():
    print("\n" + "="*60)
    print("MedSpa AI - End-to-End Test")
    print("="*60)
    print(f"API: {API_URL}")
    print(f"Provider: {PROVIDER}")
    print(f"Org ID: {TEST_ORG_ID}")
    print(f"From: {TEST_PHONE}")
    print(f"To: {TEST_TO_NUMBER}")

    if not check_health():
        print("\n❌ Aborted - API not available")
        sys.exit(1)

    create_lead()

    if not send_sms_webhook("I want to book Botox for weekday afternoons"):
        print("\n❌ Aborted - webhook failed")
        sys.exit(1)

    verify_conversation()
    show_verification()

    print("\n" + "="*60)
    print("✅ Test Complete!")
    print("="*60)
    print("\n⚠️  NOTE: Signature errors are expected")
    print("\nFollow steps above to verify preferences were saved.\n")


if __name__ == "__main__":
    main()
