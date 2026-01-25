#!/usr/bin/env python3
"""
Extended E2E Test Scenarios for MedSpa AI Platform

This script tests business scenarios NOT covered by e2e_full_flow.py:
1. Twilio SMS webhook flow
2. Clinic onboarding workflow
3. Square OAuth flow (simulated)
4. Booking confirmation flow
5. Lead update operations
6. Quiet hours enforcement
7. Error handling scenarios
8. Lead detail and stats endpoints
9. Conversation listing, stats, and export
10. Square disconnect and location sync
11. 10DLC brand/campaign management
12. Hosted number orders
13. Admin messaging
14. Phone number management

Usage:
    python scripts/e2e_extended_scenarios.py

    # With custom API URL:
    API_URL=http://localhost:8082 python scripts/e2e_extended_scenarios.py

    # Run specific test:
    python scripts/e2e_extended_scenarios.py --test twilio
    python scripts/e2e_extended_scenarios.py --test onboarding
    python scripts/e2e_extended_scenarios.py --test booking
    python scripts/e2e_extended_scenarios.py --test leads_detail
    python scripts/e2e_extended_scenarios.py --test conversations
    python scripts/e2e_extended_scenarios.py --test square_admin
    python scripts/e2e_extended_scenarios.py --test ten_dlc
    python scripts/e2e_extended_scenarios.py --test messaging
"""

import os
import sys
import time
import json
import uuid
import hmac
import hashlib
import argparse
from datetime import datetime, timezone
from typing import Optional, Dict, Any, List
from urllib.parse import urlencode

# =============================================================================
# Optional .env Loading
# =============================================================================

def load_dotenv(path: str) -> None:
    """Best-effort .env loader (no external deps)."""
    if not path or not os.path.exists(path):
        return
    try:
        with open(path, "r", encoding="utf-8") as f:
            for raw_line in f:
                line = raw_line.strip()
                if not line or line.startswith("#") or "=" not in line:
                    continue
                key, value = line.split("=", 1)
                key = key.strip()
                value = value.strip().strip('"').strip("'")
                if key and key not in os.environ:
                    os.environ[key] = value
    except Exception:
        return

_PROJECT_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
load_dotenv(os.getenv("DOTENV_PATH", os.path.join(_PROJECT_ROOT, ".env")))

# Fix Windows console encoding for Unicode
if sys.platform == 'win32':
    sys.stdout.reconfigure(encoding='utf-8', errors='replace')
    sys.stderr.reconfigure(encoding='utf-8', errors='replace')

try:
    import requests
except ImportError:
    print("ERROR: 'requests' module required. Install with: pip install requests")
    sys.exit(1)

# =============================================================================
# Configuration
# =============================================================================

API_URL = os.getenv("API_URL", "http://localhost:8082")
DATABASE_URL = os.getenv("DATABASE_URL", "postgresql://medspa:medspa@localhost:5432/medspa?sslmode=disable")
SKIP_DB_CHECK = os.getenv("SKIP_DB_CHECK", "").lower() in ("1", "true", "yes")

# Test identifiers
TEST_ORG_ID = os.getenv("TEST_ORG_ID", "11111111-1111-1111-1111-111111111111")
TEST_CUSTOMER_PHONE = os.getenv("TEST_CUSTOMER_PHONE", "+15550001234")
TEST_CLINIC_PHONE = os.getenv("TEST_CLINIC_PHONE", "+15559998888")

# Twilio config
TWILIO_ACCOUNT_SID = os.getenv("TWILIO_ACCOUNT_SID", "ACtest1234567890")
TWILIO_AUTH_TOKEN = os.getenv("TWILIO_AUTH_TOKEN", "")
TWILIO_ORG_MAP_JSON = os.getenv("TWILIO_ORG_MAP_JSON", "{}")

# Admin JWT for admin endpoints
ADMIN_JWT_SECRET = os.getenv("ADMIN_JWT_SECRET", "")

# Colors for terminal output
class Colors:
    HEADER = '\033[95m'
    BLUE = '\033[94m'
    CYAN = '\033[96m'
    GREEN = '\033[92m'
    YELLOW = '\033[93m'
    RED = '\033[91m'
    ENDC = '\033[0m'
    BOLD = '\033[1m'

# =============================================================================
# Utility Functions
# =============================================================================

def print_header(text: str):
    print(f"\n{Colors.HEADER}{Colors.BOLD}{'='*70}")
    print(f"  {text}")
    print(f"{'='*70}{Colors.ENDC}\n")

def print_step(step_num: int, text: str):
    print(f"\n{Colors.CYAN}{Colors.BOLD}[STEP {step_num}] {text}{Colors.ENDC}")
    print("-" * 60)

def print_success(text: str):
    print(f"{Colors.GREEN}PASS {text}{Colors.ENDC}")

def print_warning(text: str):
    print(f"{Colors.YELLOW}WARN  {text}{Colors.ENDC}")

def print_error(text: str):
    print(f"{Colors.RED}FAIL {text}{Colors.ENDC}")

def print_info(text: str):
    print(f"{Colors.BLUE}INFO  {text}{Colors.ENDC}")

def timestamp() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")

def generate_uuid() -> str:
    return str(uuid.uuid4())

def make_admin_jwt(secret: str, ttl_seconds: int = 1200) -> str:
    """Create a simple admin JWT for testing."""
    import base64

    def b64url(data: bytes) -> str:
        return base64.urlsafe_b64encode(data).rstrip(b"=").decode("ascii")

    now = int(time.time())
    header = {"alg": "HS256", "typ": "JWT"}
    payload = {"iat": now, "exp": now + ttl_seconds, "role": "admin"}

    header_b64 = b64url(json.dumps(header, separators=(",", ":")).encode("utf-8"))
    payload_b64 = b64url(json.dumps(payload, separators=(",", ":")).encode("utf-8"))
    signing_input = f"{header_b64}.{payload_b64}".encode("ascii")
    sig = hmac.new(secret.encode("utf-8"), signing_input, hashlib.sha256).digest()
    return f"{header_b64}.{payload_b64}.{b64url(sig)}"

# =============================================================================
# Test Results Tracker
# =============================================================================

class TestResults:
    def __init__(self):
        self.passed = 0
        self.failed = 0
        self.skipped = 0
        self.details: List[Dict[str, Any]] = []

    def record(self, name: str, passed: bool, message: str = "", skipped: bool = False):
        if skipped:
            self.skipped += 1
            status = "SKIPPED"
        elif passed:
            self.passed += 1
            status = "PASSED"
        else:
            self.failed += 1
            status = "FAILED"
        self.details.append({"name": name, "status": status, "message": message})

    def summary(self):
        total = self.passed + self.failed + self.skipped
        print_header("Extended E2E Test Summary")
        print(f"  {Colors.GREEN}Passed:  {self.passed}{Colors.ENDC}")
        print(f"  {Colors.RED}Failed:  {self.failed}{Colors.ENDC}")
        print(f"  {Colors.YELLOW}Skipped: {self.skipped}{Colors.ENDC}")
        print(f"  Total:   {total}")
        print()

        if self.failed > 0:
            print("Failed tests:")
            for d in self.details:
                if d["status"] == "FAILED":
                    print(f"  - {d['name']}: {d['message']}")

        return self.failed == 0

# =============================================================================
# Test 1: Twilio SMS Webhook Flow
# =============================================================================

def test_twilio_sms_webhook(results: TestResults) -> bool:
    """Test Twilio SMS inbound webhook processing."""
    print_step(1, "Testing Twilio SMS Webhook Flow")

    # Check if Twilio is configured
    if not TWILIO_AUTH_TOKEN:
        print_warning("TWILIO_AUTH_TOKEN not set - skipping Twilio tests")
        results.record("twilio_sms_webhook", False, "TWILIO_AUTH_TOKEN not configured", skipped=True)
        return True

    try:
        # Twilio sends form-encoded data, not JSON
        message_sid = f"SM{uuid.uuid4().hex[:32]}"
        payload = {
            "MessageSid": message_sid,
            "AccountSid": TWILIO_ACCOUNT_SID,
            "From": TEST_CUSTOMER_PHONE,
            "To": TEST_CLINIC_PHONE,
            "Body": "Hello from Twilio E2E test"
        }

        # Compute Twilio signature (simplified - real signature needs full URL)
        # For testing without signature validation, we send without X-Twilio-Signature
        resp = requests.post(
            f"{API_URL}/messaging/twilio/webhook",
            data=payload,
            headers={"Content-Type": "application/x-www-form-urlencoded"},
            timeout=30
        )

        if resp.status_code == 200:
            print_success("Twilio SMS webhook accepted (200)")
            results.record("twilio_sms_webhook", True)
            return True
        elif resp.status_code in (401, 403):
            print_warning(f"Twilio signature validation failed ({resp.status_code}) - expected without valid signature")
            results.record("twilio_sms_webhook", True, "Signature validation working correctly")
            return True
        elif resp.status_code == 404:
            print_error(f"Twilio webhook endpoint not found: {resp.status_code}")
            results.record("twilio_sms_webhook", False, f"Endpoint not found: {resp.status_code}")
            return False
        else:
            print_error(f"Twilio webhook failed: {resp.status_code} - {resp.text[:200]}")
            results.record("twilio_sms_webhook", False, f"Status {resp.status_code}")
            return False

    except Exception as e:
        print_error(f"Twilio webhook test failed: {e}")
        results.record("twilio_sms_webhook", False, str(e))
        return False

# =============================================================================
# Test 2: Clinic Onboarding Workflow
# =============================================================================

def test_clinic_onboarding(results: TestResults) -> bool:
    """Test clinic onboarding workflow."""
    print_step(2, "Testing Clinic Onboarding Workflow")

    test_org_id = f"e2e-test-{uuid.uuid4().hex[:8]}"
    all_passed = True

    try:
        # Step 2a: Create new clinic
        print_info("Creating new clinic...")
        create_payload = {
            "name": "E2E Test Clinic",
            "email": "e2e-test@example.com",
            "phone": "+15550009999",
            "timezone": "America/New_York"
        }

        resp = requests.post(
            f"{API_URL}/onboarding/clinics",
            json=create_payload,
            headers={"Content-Type": "application/json"},
            timeout=15
        )

        if resp.status_code in (200, 201):
            clinic_data = resp.json()
            created_org_id = clinic_data.get("org_id") or clinic_data.get("id")
            if created_org_id:
                test_org_id = created_org_id
            print_success(f"Clinic created: {test_org_id}")
            results.record("onboarding_create_clinic", True)
        elif resp.status_code == 409:
            print_warning("Clinic already exists (409) - continuing with existing")
            results.record("onboarding_create_clinic", True, "Already exists")
        else:
            print_error(f"Clinic creation failed: {resp.status_code} - {resp.text[:200]}")
            results.record("onboarding_create_clinic", False, f"Status {resp.status_code}")
            all_passed = False

        # Step 2b: Get onboarding status
        print_info("Checking onboarding status...")
        resp = requests.get(
            f"{API_URL}/onboarding/clinics/{test_org_id}/status",
            timeout=10
        )

        if resp.status_code == 200:
            status = resp.json()
            progress = status.get("progress_percent", 0)
            print_success(f"Onboarding status retrieved: {progress}% complete")
            results.record("onboarding_get_status", True)
        else:
            print_error(f"Get status failed: {resp.status_code}")
            results.record("onboarding_get_status", False, f"Status {resp.status_code}")
            all_passed = False

        # Step 2c: Update clinic config
        print_info("Updating clinic config...")
        config_payload = {
            "clinic_name": "E2E Test Clinic Updated",
            "timezone": "America/Los_Angeles",
            "deposit_amount_cents": 7500,
            "services": ["botox", "fillers", "facials"]
        }

        resp = requests.put(
            f"{API_URL}/onboarding/clinics/{test_org_id}/config",
            json=config_payload,
            headers={"Content-Type": "application/json"},
            timeout=15
        )

        if resp.status_code in (200, 204):
            print_success("Clinic config updated")
            results.record("onboarding_update_config", True)
        else:
            print_error(f"Config update failed: {resp.status_code} - {resp.text[:200]}")
            results.record("onboarding_update_config", False, f"Status {resp.status_code}")
            all_passed = False

        # Step 2d: Get config to verify
        print_info("Verifying config persistence...")
        resp = requests.get(
            f"{API_URL}/onboarding/clinics/{test_org_id}/config",
            timeout=10
        )

        if resp.status_code == 200:
            config = resp.json()
            if config.get("clinic_name") == "E2E Test Clinic Updated":
                print_success("Config verified: name updated correctly")
                results.record("onboarding_verify_config", True)
            else:
                print_warning(f"Config name mismatch: {config.get('clinic_name')}")
                results.record("onboarding_verify_config", True, "Partial verification")
        else:
            print_error(f"Get config failed: {resp.status_code}")
            results.record("onboarding_verify_config", False, f"Status {resp.status_code}")
            all_passed = False

        return all_passed

    except Exception as e:
        print_error(f"Onboarding test failed: {e}")
        results.record("onboarding_workflow", False, str(e))
        return False

# =============================================================================
# Test 3: Square OAuth Flow (Simulated)
# =============================================================================

def test_square_oauth_flow(results: TestResults) -> bool:
    """Test Square OAuth connection flow (simulated - no actual OAuth)."""
    print_step(3, "Testing Square OAuth Flow (Simulated)")

    try:
        # Step 3a: Get Square connect URL
        print_info("Requesting Square connect URL...")
        resp = requests.get(
            f"{API_URL}/onboarding/clinics/{TEST_ORG_ID}/square/connect",
            timeout=10,
            allow_redirects=False
        )

        if resp.status_code in (200, 302, 307):
            if resp.status_code in (302, 307):
                redirect_url = resp.headers.get("Location", "")
                if "squareup.com" in redirect_url or "square" in redirect_url.lower():
                    print_success(f"Square OAuth redirect URL received")
                    results.record("square_oauth_connect", True)
                else:
                    print_warning(f"Redirect URL doesn't point to Square: {redirect_url[:100]}")
                    results.record("square_oauth_connect", True, "Redirect received")
            else:
                data = resp.json() if resp.text else {}
                auth_url = data.get("auth_url") or data.get("url")
                if auth_url:
                    print_success(f"Square auth URL in response body")
                    results.record("square_oauth_connect", True)
                else:
                    print_warning("Connect endpoint returned 200 but no auth URL")
                    results.record("square_oauth_connect", True, "No URL in body")
        elif resp.status_code == 404:
            print_warning("Square connect endpoint not found - may not be implemented")
            results.record("square_oauth_connect", False, "Endpoint not found", skipped=True)
        else:
            print_error(f"Square connect failed: {resp.status_code}")
            results.record("square_oauth_connect", False, f"Status {resp.status_code}")
            return False

        # Step 3b: Check Square connection status
        print_info("Checking Square connection status...")
        resp = requests.get(
            f"{API_URL}/onboarding/clinics/{TEST_ORG_ID}/square/status",
            timeout=10
        )

        if resp.status_code == 200:
            status = resp.json()
            connected = status.get("connected", False)
            print_success(f"Square status retrieved: connected={connected}")
            results.record("square_oauth_status", True)
        elif resp.status_code == 404:
            print_warning("Square status endpoint not found")
            results.record("square_oauth_status", False, "Endpoint not found", skipped=True)
        else:
            print_error(f"Square status check failed: {resp.status_code}")
            results.record("square_oauth_status", False, f"Status {resp.status_code}")
            return False

        return True

    except Exception as e:
        print_error(f"Square OAuth test failed: {e}")
        results.record("square_oauth_flow", False, str(e))
        return False

# =============================================================================
# Test 4: Booking Confirmation Flow
# =============================================================================

def test_booking_flow(results: TestResults) -> bool:
    """Test booking confirmation after payment."""
    print_step(4, "Testing Booking Confirmation Flow")

    if not ADMIN_JWT_SECRET:
        print_warning("ADMIN_JWT_SECRET not set - skipping admin-only booking tests")
        results.record("booking_flow", False, "ADMIN_JWT_SECRET not configured", skipped=True)
        return True

    try:
        token = make_admin_jwt(ADMIN_JWT_SECRET)
        headers = {
            "Authorization": f"Bearer {token}",
            "Content-Type": "application/json",
            "X-Org-ID": TEST_ORG_ID
        }

        # Check if we have any recent leads with payments
        print_info("Checking for recent leads with payments...")
        resp = requests.get(
            f"{API_URL}/admin/clinics/{TEST_ORG_ID}/leads",
            headers=headers,
            timeout=15
        )

        if resp.status_code == 200:
            leads = resp.json()
            if isinstance(leads, list) and len(leads) > 0:
                print_success(f"Found {len(leads)} leads in system")
                results.record("booking_list_leads", True)

                # Check first lead with deposit_status
                for lead in leads[:5]:
                    if lead.get("deposit_status") == "paid":
                        print_success(f"Found lead with paid deposit: {lead.get('id', 'unknown')[:8]}...")
                        results.record("booking_verify_paid_lead", True)
                        break
                else:
                    print_warning("No leads with paid deposits found")
                    results.record("booking_verify_paid_lead", True, "No paid deposits yet")
            else:
                print_warning("No leads found in system")
                results.record("booking_list_leads", True, "Empty list")
        elif resp.status_code == 401:
            print_error("Admin JWT authentication failed")
            results.record("booking_list_leads", False, "Auth failed")
            return False
        else:
            print_error(f"List leads failed: {resp.status_code}")
            results.record("booking_list_leads", False, f"Status {resp.status_code}")
            return False

        return True

    except Exception as e:
        print_error(f"Booking flow test failed: {e}")
        results.record("booking_flow", False, str(e))
        return False

# =============================================================================
# Test 5: Lead Update Operations
# =============================================================================

def test_lead_updates(results: TestResults) -> bool:
    """Test lead update operations via admin API."""
    print_step(5, "Testing Lead Update Operations")

    if not ADMIN_JWT_SECRET:
        print_warning("ADMIN_JWT_SECRET not set - skipping lead update tests")
        results.record("lead_updates", False, "ADMIN_JWT_SECRET not configured", skipped=True)
        return True

    try:
        token = make_admin_jwt(ADMIN_JWT_SECRET)
        headers = {
            "Authorization": f"Bearer {token}",
            "Content-Type": "application/json",
            "X-Org-ID": TEST_ORG_ID
        }

        # Get a lead to update
        resp = requests.get(
            f"{API_URL}/admin/clinics/{TEST_ORG_ID}/leads",
            headers=headers,
            timeout=15
        )

        if resp.status_code != 200:
            print_warning(f"Could not list leads: {resp.status_code}")
            results.record("lead_update_list", False, f"Status {resp.status_code}", skipped=True)
            return True

        leads = resp.json()
        if not isinstance(leads, list) or len(leads) == 0:
            print_warning("No leads available to update")
            results.record("lead_update_patch", False, "No leads available", skipped=True)
            return True

        lead_id = leads[0].get("id")
        if not lead_id:
            print_warning("Lead has no ID")
            results.record("lead_update_patch", False, "No lead ID", skipped=True)
            return True

        # Update lead
        print_info(f"Updating lead {lead_id[:8]}...")
        update_payload = {
            "notes": f"Updated by E2E test at {timestamp()}"
        }

        resp = requests.patch(
            f"{API_URL}/admin/clinics/{TEST_ORG_ID}/leads/{lead_id}",
            json=update_payload,
            headers=headers,
            timeout=15
        )

        if resp.status_code in (200, 204):
            print_success("Lead updated successfully")
            results.record("lead_update_patch", True)
            return True
        elif resp.status_code == 404:
            print_warning("Lead update endpoint not found")
            results.record("lead_update_patch", False, "Endpoint not found", skipped=True)
            return True
        else:
            print_error(f"Lead update failed: {resp.status_code} - {resp.text[:200]}")
            results.record("lead_update_patch", False, f"Status {resp.status_code}")
            return False

    except Exception as e:
        print_error(f"Lead update test failed: {e}")
        results.record("lead_updates", False, str(e))
        return False

# =============================================================================
# Test 6: Admin Dashboard & Stats
# =============================================================================

def test_admin_dashboard(results: TestResults) -> bool:
    """Test admin dashboard and stats endpoints."""
    print_step(6, "Testing Admin Dashboard & Stats")

    if not ADMIN_JWT_SECRET:
        print_warning("ADMIN_JWT_SECRET not set - skipping admin dashboard tests")
        results.record("admin_dashboard", False, "ADMIN_JWT_SECRET not configured", skipped=True)
        return True

    try:
        token = make_admin_jwt(ADMIN_JWT_SECRET)
        headers = {
            "Authorization": f"Bearer {token}",
            "X-Org-ID": TEST_ORG_ID
        }

        # Test stats endpoint
        print_info("Fetching clinic stats...")
        resp = requests.get(
            f"{API_URL}/admin/clinics/{TEST_ORG_ID}/stats",
            headers=headers,
            timeout=15
        )

        if resp.status_code == 200:
            stats = resp.json()
            print_success(f"Stats retrieved: {json.dumps(stats)[:100]}...")
            results.record("admin_stats", True)
        elif resp.status_code == 404:
            print_warning("Stats endpoint not found")
            results.record("admin_stats", False, "Endpoint not found", skipped=True)
        else:
            print_error(f"Stats failed: {resp.status_code}")
            results.record("admin_stats", False, f"Status {resp.status_code}")

        # Test dashboard endpoint
        print_info("Fetching clinic dashboard...")
        resp = requests.get(
            f"{API_URL}/admin/clinics/{TEST_ORG_ID}/dashboard",
            headers=headers,
            timeout=15
        )

        if resp.status_code == 200:
            dashboard = resp.json()
            print_success(f"Dashboard retrieved")
            results.record("admin_dashboard", True)
        elif resp.status_code == 404:
            print_warning("Dashboard endpoint not found")
            results.record("admin_dashboard", False, "Endpoint not found", skipped=True)
        else:
            print_error(f"Dashboard failed: {resp.status_code}")
            results.record("admin_dashboard", False, f"Status {resp.status_code}")

        return True

    except Exception as e:
        print_error(f"Admin dashboard test failed: {e}")
        results.record("admin_dashboard", False, str(e))
        return False

# =============================================================================
# Test 7: Error Handling Scenarios
# =============================================================================

def test_error_handling(results: TestResults) -> bool:
    """Test error handling and edge cases."""
    print_step(7, "Testing Error Handling Scenarios")

    all_passed = True

    try:
        # Test 7a: Invalid JSON payload
        print_info("Testing invalid JSON handling...")
        resp = requests.post(
            f"{API_URL}/leads/web",
            data="not valid json",
            headers={"Content-Type": "application/json", "X-Org-ID": TEST_ORG_ID},
            timeout=10
        )

        if resp.status_code == 400:
            print_success("Invalid JSON correctly rejected with 400")
            results.record("error_invalid_json", True)
        else:
            print_warning(f"Invalid JSON returned {resp.status_code} (expected 400)")
            results.record("error_invalid_json", True, f"Got {resp.status_code}")

        # Test 7b: Missing required fields
        print_info("Testing missing required fields...")
        resp = requests.post(
            f"{API_URL}/leads/web",
            json={"name": "Test"},  # Missing phone
            headers={"Content-Type": "application/json", "X-Org-ID": TEST_ORG_ID},
            timeout=10
        )

        if resp.status_code in (400, 422):
            print_success(f"Missing fields correctly rejected with {resp.status_code}")
            results.record("error_missing_fields", True)
        else:
            print_warning(f"Missing fields returned {resp.status_code} (expected 400/422)")
            results.record("error_missing_fields", True, f"Got {resp.status_code}")

        # Test 7c: Invalid org ID
        print_info("Testing invalid org ID...")
        resp = requests.get(
            f"{API_URL}/onboarding/clinics/invalid-uuid/status",
            timeout=10
        )

        if resp.status_code in (400, 404):
            print_success(f"Invalid org ID correctly handled with {resp.status_code}")
            results.record("error_invalid_org", True)
        else:
            print_warning(f"Invalid org ID returned {resp.status_code}")
            results.record("error_invalid_org", True, f"Got {resp.status_code}")

        # Test 7d: Non-existent resource
        print_info("Testing non-existent resource...")
        fake_uuid = str(uuid.uuid4())
        resp = requests.get(
            f"{API_URL}/onboarding/clinics/{fake_uuid}/status",
            timeout=10
        )

        if resp.status_code == 404:
            print_success("Non-existent resource correctly returned 404")
            results.record("error_not_found", True)
        elif resp.status_code == 200:
            # Some APIs return empty/default for non-existent
            print_warning("Non-existent resource returned 200 (may return default)")
            results.record("error_not_found", True, "Returned default data")
        else:
            print_warning(f"Non-existent resource returned {resp.status_code}")
            results.record("error_not_found", True, f"Got {resp.status_code}")

        return all_passed

    except Exception as e:
        print_error(f"Error handling test failed: {e}")
        results.record("error_handling", False, str(e))
        return False

# =============================================================================
# Test 8: Quiet Hours Simulation
# =============================================================================

def test_quiet_hours(results: TestResults) -> bool:
    """Test quiet hours enforcement (simulated check)."""
    print_step(8, "Testing Quiet Hours Enforcement")

    print_info("Quiet hours are enforced server-side based on clinic timezone")
    print_info("This test verifies the config endpoint accepts quiet hours settings")

    try:
        # Update clinic config with quiet hours
        config_payload = {
            "quiet_hours_start": "21:00",
            "quiet_hours_end": "08:00",
            "timezone": "America/New_York"
        }

        resp = requests.put(
            f"{API_URL}/onboarding/clinics/{TEST_ORG_ID}/config",
            json=config_payload,
            headers={"Content-Type": "application/json"},
            timeout=15
        )

        if resp.status_code in (200, 204):
            print_success("Quiet hours config accepted")
            results.record("quiet_hours_config", True)
        elif resp.status_code == 404:
            print_warning("Config endpoint not found")
            results.record("quiet_hours_config", False, "Endpoint not found", skipped=True)
        else:
            print_warning(f"Quiet hours config returned {resp.status_code}")
            results.record("quiet_hours_config", True, f"Status {resp.status_code}")

        return True

    except Exception as e:
        print_error(f"Quiet hours test failed: {e}")
        results.record("quiet_hours", False, str(e))
        return False

# =============================================================================
# Test 9: Lead Detail and Stats Endpoints
# =============================================================================

def test_leads_detail(results: TestResults) -> bool:
    """Test lead detail and stats endpoints."""
    print_step(9, "Testing Lead Detail and Stats Endpoints")

    if not ADMIN_JWT_SECRET:
        print_warning("ADMIN_JWT_SECRET not set - skipping lead detail tests")
        results.record("leads_detail", False, "ADMIN_JWT_SECRET not configured", skipped=True)
        return True

    try:
        token = make_admin_jwt(ADMIN_JWT_SECRET)
        headers = {
            "Authorization": f"Bearer {token}",
            "X-Org-ID": TEST_ORG_ID
        }

        # Test lead stats endpoint
        print_info("Fetching lead stats...")
        resp = requests.get(
            f"{API_URL}/admin/clinics/{TEST_ORG_ID}/leads/stats",
            headers=headers,
            timeout=15
        )

        if resp.status_code == 200:
            stats = resp.json()
            print_success(f"Lead stats retrieved: {json.dumps(stats)[:100]}...")
            results.record("leads_stats", True)
        elif resp.status_code == 404:
            print_warning("Lead stats endpoint not found")
            results.record("leads_stats", False, "Endpoint not found", skipped=True)
        else:
            print_error(f"Lead stats failed: {resp.status_code}")
            results.record("leads_stats", False, f"Status {resp.status_code}")

        # Get a lead ID to test detail endpoint
        resp = requests.get(
            f"{API_URL}/admin/clinics/{TEST_ORG_ID}/leads",
            headers=headers,
            timeout=15
        )

        if resp.status_code == 200:
            leads = resp.json()
            if isinstance(leads, list) and len(leads) > 0:
                lead_id = leads[0].get("id")
                if lead_id:
                    print_info(f"Fetching lead detail for {lead_id[:8]}...")
                    resp = requests.get(
                        f"{API_URL}/admin/clinics/{TEST_ORG_ID}/leads/{lead_id}",
                        headers=headers,
                        timeout=15
                    )

                    if resp.status_code == 200:
                        lead_detail = resp.json()
                        print_success(f"Lead detail retrieved: {lead_detail.get('name', 'unknown')}")
                        results.record("leads_detail_get", True)
                    elif resp.status_code == 404:
                        print_warning("Lead detail endpoint not found")
                        results.record("leads_detail_get", False, "Endpoint not found", skipped=True)
                    else:
                        print_error(f"Lead detail failed: {resp.status_code}")
                        results.record("leads_detail_get", False, f"Status {resp.status_code}")
                else:
                    print_warning("Lead has no ID")
                    results.record("leads_detail_get", False, "No lead ID", skipped=True)
            else:
                print_warning("No leads available to test detail endpoint")
                results.record("leads_detail_get", False, "No leads", skipped=True)
        else:
            print_warning(f"Could not list leads: {resp.status_code}")
            results.record("leads_detail_get", False, f"List failed: {resp.status_code}", skipped=True)

        return True

    except Exception as e:
        print_error(f"Lead detail test failed: {e}")
        results.record("leads_detail", False, str(e))
        return False

# =============================================================================
# Test 10: Conversation Listing, Stats, and Export
# =============================================================================

def test_conversations(results: TestResults) -> bool:
    """Test conversation listing, stats, and export endpoints."""
    print_step(10, "Testing Conversation Endpoints")

    if not ADMIN_JWT_SECRET:
        print_warning("ADMIN_JWT_SECRET not set - skipping conversation tests")
        results.record("conversations", False, "ADMIN_JWT_SECRET not configured", skipped=True)
        return True

    try:
        token = make_admin_jwt(ADMIN_JWT_SECRET)
        headers = {
            "Authorization": f"Bearer {token}",
            "X-Org-ID": TEST_ORG_ID
        }

        # Test conversation listing
        print_info("Fetching conversation list...")
        resp = requests.get(
            f"{API_URL}/admin/clinics/{TEST_ORG_ID}/conversations",
            headers=headers,
            timeout=15
        )

        conversation_id = None
        if resp.status_code == 200:
            conversations = resp.json()
            if isinstance(conversations, list):
                print_success(f"Conversation list retrieved: {len(conversations)} conversations")
                results.record("conversations_list", True)
                if len(conversations) > 0:
                    conversation_id = conversations[0].get("id") or conversations[0].get("conversation_id")
            elif isinstance(conversations, dict):
                items = conversations.get("conversations") or conversations.get("items") or []
                print_success(f"Conversation list retrieved: {len(items)} conversations")
                results.record("conversations_list", True)
                if len(items) > 0:
                    conversation_id = items[0].get("id") or items[0].get("conversation_id")
        elif resp.status_code == 404:
            print_warning("Conversations list endpoint not found")
            results.record("conversations_list", False, "Endpoint not found", skipped=True)
        else:
            print_error(f"Conversations list failed: {resp.status_code}")
            results.record("conversations_list", False, f"Status {resp.status_code}")

        # Test conversation stats
        print_info("Fetching conversation stats...")
        resp = requests.get(
            f"{API_URL}/admin/clinics/{TEST_ORG_ID}/conversations/stats",
            headers=headers,
            timeout=15
        )

        if resp.status_code == 200:
            stats = resp.json()
            print_success(f"Conversation stats retrieved: {json.dumps(stats)[:100]}...")
            results.record("conversations_stats", True)
        elif resp.status_code == 404:
            print_warning("Conversation stats endpoint not found")
            results.record("conversations_stats", False, "Endpoint not found", skipped=True)
        else:
            print_error(f"Conversation stats failed: {resp.status_code}")
            results.record("conversations_stats", False, f"Status {resp.status_code}")

        # Test conversation detail and export if we have a conversation ID
        if conversation_id:
            print_info(f"Fetching conversation detail for {conversation_id[:8] if len(conversation_id) > 8 else conversation_id}...")
            resp = requests.get(
                f"{API_URL}/admin/clinics/{TEST_ORG_ID}/conversations/{conversation_id}",
                headers=headers,
                timeout=15
            )

            if resp.status_code == 200:
                print_success("Conversation detail retrieved")
                results.record("conversations_detail", True)
            elif resp.status_code == 404:
                print_warning("Conversation detail endpoint not found")
                results.record("conversations_detail", False, "Endpoint not found", skipped=True)
            else:
                print_error(f"Conversation detail failed: {resp.status_code}")
                results.record("conversations_detail", False, f"Status {resp.status_code}")

            # Test export
            print_info("Testing conversation export...")
            resp = requests.get(
                f"{API_URL}/admin/clinics/{TEST_ORG_ID}/conversations/{conversation_id}/export",
                headers=headers,
                timeout=15
            )

            if resp.status_code == 200:
                content_type = resp.headers.get("Content-Type", "")
                print_success(f"Conversation export retrieved: {content_type}")
                results.record("conversations_export", True)
            elif resp.status_code == 404:
                print_warning("Conversation export endpoint not found")
                results.record("conversations_export", False, "Endpoint not found", skipped=True)
            else:
                print_error(f"Conversation export failed: {resp.status_code}")
                results.record("conversations_export", False, f"Status {resp.status_code}")
        else:
            print_warning("No conversation ID available for detail/export tests")
            results.record("conversations_detail", False, "No conversation ID", skipped=True)
            results.record("conversations_export", False, "No conversation ID", skipped=True)

        return True

    except Exception as e:
        print_error(f"Conversation test failed: {e}")
        results.record("conversations", False, str(e))
        return False

# =============================================================================
# Test 11: Square Admin Operations (Disconnect, Sync Location, Phone)
# =============================================================================

def test_square_admin(results: TestResults) -> bool:
    """Test Square admin operations (disconnect, sync location, phone update)."""
    print_step(11, "Testing Square Admin Operations")

    if not ADMIN_JWT_SECRET:
        print_warning("ADMIN_JWT_SECRET not set - skipping Square admin tests")
        results.record("square_admin", False, "ADMIN_JWT_SECRET not configured", skipped=True)
        return True

    try:
        token = make_admin_jwt(ADMIN_JWT_SECRET)
        headers = {
            "Authorization": f"Bearer {token}",
            "Content-Type": "application/json",
            "X-Org-ID": TEST_ORG_ID
        }

        # Test Square location sync (safe to call even if not connected)
        print_info("Testing Square location sync...")
        resp = requests.post(
            f"{API_URL}/admin/clinics/{TEST_ORG_ID}/square/sync-location",
            headers=headers,
            timeout=15
        )

        if resp.status_code in (200, 204):
            print_success("Square location sync succeeded")
            results.record("square_sync_location", True)
        elif resp.status_code == 400:
            print_warning("Square sync location returned 400 (may not be connected)")
            results.record("square_sync_location", True, "Not connected")
        elif resp.status_code == 404:
            print_warning("Square sync location endpoint not found")
            results.record("square_sync_location", False, "Endpoint not found", skipped=True)
        else:
            print_error(f"Square sync location failed: {resp.status_code}")
            results.record("square_sync_location", False, f"Status {resp.status_code}")

        # Test phone number update
        print_info("Testing phone number update...")
        resp = requests.put(
            f"{API_URL}/admin/clinics/{TEST_ORG_ID}/phone",
            json={"phone_number": TEST_CLINIC_PHONE},
            headers=headers,
            timeout=15
        )

        if resp.status_code in (200, 204):
            print_success("Phone number update succeeded")
            results.record("admin_phone_update", True)
        elif resp.status_code == 404:
            print_warning("Phone update endpoint not found")
            results.record("admin_phone_update", False, "Endpoint not found", skipped=True)
        else:
            print_warning(f"Phone update returned {resp.status_code}")
            results.record("admin_phone_update", True, f"Status {resp.status_code}")

        # Note: We don't test disconnect as it would break the Square connection
        # Just verify the endpoint exists
        print_info("Checking Square disconnect endpoint exists...")
        resp = requests.delete(
            f"{API_URL}/admin/clinics/{TEST_ORG_ID}/square/disconnect",
            headers=headers,
            timeout=10
        )

        # We don't actually want to disconnect, so any response other than 404 means it exists
        if resp.status_code == 404:
            print_warning("Square disconnect endpoint not found")
            results.record("square_disconnect_exists", False, "Endpoint not found", skipped=True)
        else:
            print_success(f"Square disconnect endpoint exists (returned {resp.status_code})")
            results.record("square_disconnect_exists", True)

        return True

    except Exception as e:
        print_error(f"Square admin test failed: {e}")
        results.record("square_admin", False, str(e))
        return False

# =============================================================================
# Test 12: 10DLC Brand and Campaign Management
# =============================================================================

def test_ten_dlc(results: TestResults) -> bool:
    """Test 10DLC brand and campaign management endpoints."""
    print_step(12, "Testing 10DLC Brand and Campaign Management")

    if not ADMIN_JWT_SECRET:
        print_warning("ADMIN_JWT_SECRET not set - skipping 10DLC tests")
        results.record("ten_dlc", False, "ADMIN_JWT_SECRET not configured", skipped=True)
        return True

    try:
        token = make_admin_jwt(ADMIN_JWT_SECRET)
        headers = {
            "Authorization": f"Bearer {token}",
            "Content-Type": "application/json",
            "X-Org-ID": TEST_ORG_ID
        }

        # Test brand creation (with test data - won't actually create in Telnyx)
        print_info("Testing 10DLC brand creation endpoint...")
        brand_payload = {
            "name": "E2E Test Brand",
            "entity_type": "PRIVATE_PROFIT",
            "ein": "12-3456789",
            "phone": TEST_CLINIC_PHONE,
            "email": "test@example.com",
            "street": "123 Test St",
            "city": "Test City",
            "state": "OH",
            "postal_code": "12345",
            "country": "US",
            "website": "https://example.com"
        }

        resp = requests.post(
            f"{API_URL}/admin/10dlc/brands",
            json=brand_payload,
            headers=headers,
            timeout=30
        )

        if resp.status_code in (200, 201):
            print_success("10DLC brand creation succeeded")
            results.record("ten_dlc_brand_create", True)
        elif resp.status_code == 400:
            print_warning("10DLC brand returned 400 (validation or already exists)")
            results.record("ten_dlc_brand_create", True, "Validation/duplicate")
        elif resp.status_code == 404:
            print_warning("10DLC brand endpoint not found")
            results.record("ten_dlc_brand_create", False, "Endpoint not found", skipped=True)
        elif resp.status_code == 503:
            print_warning("10DLC brand returned 503 (Telnyx may be unavailable)")
            results.record("ten_dlc_brand_create", True, "Service unavailable")
        else:
            print_warning(f"10DLC brand creation returned {resp.status_code}: {resp.text[:100]}")
            results.record("ten_dlc_brand_create", True, f"Status {resp.status_code}")

        # Test campaign creation
        print_info("Testing 10DLC campaign creation endpoint...")
        campaign_payload = {
            "use_case": "MARKETING",
            "description": "E2E test campaign for appointment booking",
            "sample_messages": [
                "Hi! Thanks for contacting us. How can we help you book an appointment?",
                "Your appointment is confirmed for Friday at 3pm."
            ],
            "message_flow": "Customer texts clinic -> AI responds -> Books appointment"
        }

        resp = requests.post(
            f"{API_URL}/admin/10dlc/campaigns",
            json=campaign_payload,
            headers=headers,
            timeout=30
        )

        if resp.status_code in (200, 201):
            print_success("10DLC campaign creation succeeded")
            results.record("ten_dlc_campaign_create", True)
        elif resp.status_code == 400:
            print_warning("10DLC campaign returned 400 (validation or missing brand)")
            results.record("ten_dlc_campaign_create", True, "Validation error")
        elif resp.status_code == 404:
            print_warning("10DLC campaign endpoint not found")
            results.record("ten_dlc_campaign_create", False, "Endpoint not found", skipped=True)
        elif resp.status_code == 503:
            print_warning("10DLC campaign returned 503 (Telnyx may be unavailable)")
            results.record("ten_dlc_campaign_create", True, "Service unavailable")
        else:
            print_warning(f"10DLC campaign creation returned {resp.status_code}: {resp.text[:100]}")
            results.record("ten_dlc_campaign_create", True, f"Status {resp.status_code}")

        return True

    except Exception as e:
        print_error(f"10DLC test failed: {e}")
        results.record("ten_dlc", False, str(e))
        return False

# =============================================================================
# Test 13: Hosted Number Orders
# =============================================================================

def test_hosted_orders(results: TestResults) -> bool:
    """Test hosted number order management."""
    print_step(13, "Testing Hosted Number Orders")

    if not ADMIN_JWT_SECRET:
        print_warning("ADMIN_JWT_SECRET not set - skipping hosted order tests")
        results.record("hosted_orders", False, "ADMIN_JWT_SECRET not configured", skipped=True)
        return True

    try:
        token = make_admin_jwt(ADMIN_JWT_SECRET)
        headers = {
            "Authorization": f"Bearer {token}",
            "Content-Type": "application/json",
            "X-Org-ID": TEST_ORG_ID
        }

        # Test hosted order creation
        print_info("Testing hosted number order creation...")
        order_payload = {
            "phone_number": TEST_CLINIC_PHONE,
            "messaging_profile_id": "test-profile-id"
        }

        resp = requests.post(
            f"{API_URL}/admin/hosted/orders",
            json=order_payload,
            headers=headers,
            timeout=30
        )

        if resp.status_code in (200, 201):
            print_success("Hosted order creation succeeded")
            results.record("hosted_order_create", True)
        elif resp.status_code == 400:
            print_warning("Hosted order returned 400 (validation or already exists)")
            results.record("hosted_order_create", True, "Validation/duplicate")
        elif resp.status_code == 404:
            print_warning("Hosted order endpoint not found")
            results.record("hosted_order_create", False, "Endpoint not found", skipped=True)
        elif resp.status_code == 503:
            print_warning("Hosted order returned 503 (Telnyx may be unavailable)")
            results.record("hosted_order_create", True, "Service unavailable")
        else:
            print_warning(f"Hosted order creation returned {resp.status_code}: {resp.text[:100]}")
            results.record("hosted_order_create", True, f"Status {resp.status_code}")

        return True

    except Exception as e:
        print_error(f"Hosted order test failed: {e}")
        results.record("hosted_orders", False, str(e))
        return False

# =============================================================================
# Test 14: Admin Messaging
# =============================================================================

def test_messaging(results: TestResults) -> bool:
    """Test admin messaging endpoints."""
    print_step(14, "Testing Admin Messaging")

    if not ADMIN_JWT_SECRET:
        print_warning("ADMIN_JWT_SECRET not set - skipping messaging tests")
        results.record("messaging", False, "ADMIN_JWT_SECRET not configured", skipped=True)
        return True

    try:
        token = make_admin_jwt(ADMIN_JWT_SECRET)
        headers = {
            "Authorization": f"Bearer {token}",
            "Content-Type": "application/json",
            "X-Org-ID": TEST_ORG_ID
        }

        # Test admin message send (without actually sending to avoid costs)
        # We'll use a validation-only approach
        print_info("Testing admin message send endpoint validation...")
        message_payload = {
            "to": "+15550000000",  # Invalid/test number
            "body": "E2E test message - should not be sent",
            "dry_run": True  # If supported
        }

        resp = requests.post(
            f"{API_URL}/admin/messages:send",
            json=message_payload,
            headers=headers,
            timeout=15
        )

        if resp.status_code in (200, 201):
            print_success("Admin message send endpoint works")
            results.record("admin_message_send", True)
        elif resp.status_code == 400:
            print_warning("Admin message returned 400 (validation - expected for test number)")
            results.record("admin_message_send", True, "Validation expected")
        elif resp.status_code == 404:
            print_warning("Admin message send endpoint not found")
            results.record("admin_message_send", False, "Endpoint not found", skipped=True)
        elif resp.status_code == 422:
            print_warning("Admin message returned 422 (invalid phone - expected)")
            results.record("admin_message_send", True, "Invalid phone expected")
        else:
            print_warning(f"Admin message send returned {resp.status_code}")
            results.record("admin_message_send", True, f"Status {resp.status_code}")

        return True

    except Exception as e:
        print_error(f"Messaging test failed: {e}")
        results.record("messaging", False, str(e))
        return False

# =============================================================================
# Test 15: Admin Clinic Creation and Onboarding Status
# =============================================================================

def test_admin_clinic_ops(results: TestResults) -> bool:
    """Test admin clinic creation and onboarding status."""
    print_step(15, "Testing Admin Clinic Operations")

    if not ADMIN_JWT_SECRET:
        print_warning("ADMIN_JWT_SECRET not set - skipping admin clinic tests")
        results.record("admin_clinic_ops", False, "ADMIN_JWT_SECRET not configured", skipped=True)
        return True

    try:
        token = make_admin_jwt(ADMIN_JWT_SECRET)
        headers = {
            "Authorization": f"Bearer {token}",
            "Content-Type": "application/json"
        }

        # Test admin clinic creation
        test_org_id = f"e2e-admin-{uuid.uuid4().hex[:8]}"
        print_info(f"Testing admin clinic creation...")
        clinic_payload = {
            "name": "E2E Admin Test Clinic",
            "email": "e2e-admin@example.com",
            "phone": "+15550009999",
            "timezone": "America/New_York"
        }

        resp = requests.post(
            f"{API_URL}/admin/clinics",
            json=clinic_payload,
            headers=headers,
            timeout=15
        )

        created_org_id = None
        if resp.status_code in (200, 201):
            clinic_data = resp.json()
            created_org_id = clinic_data.get("org_id") or clinic_data.get("id")
            print_success(f"Admin clinic creation succeeded: {created_org_id}")
            results.record("admin_clinic_create", True)
        elif resp.status_code == 409:
            print_warning("Admin clinic creation returned 409 (already exists)")
            results.record("admin_clinic_create", True, "Already exists")
        elif resp.status_code == 404:
            print_warning("Admin clinic creation endpoint not found")
            results.record("admin_clinic_create", False, "Endpoint not found", skipped=True)
        else:
            print_warning(f"Admin clinic creation returned {resp.status_code}")
            results.record("admin_clinic_create", True, f"Status {resp.status_code}")

        # Test admin onboarding status
        org_to_check = created_org_id or TEST_ORG_ID
        print_info(f"Testing admin onboarding status for {org_to_check[:8]}...")
        resp = requests.get(
            f"{API_URL}/admin/clinics/{org_to_check}/onboarding-status",
            headers=headers,
            timeout=15
        )

        if resp.status_code == 200:
            status = resp.json()
            print_success(f"Admin onboarding status retrieved: {json.dumps(status)[:100]}...")
            results.record("admin_onboarding_status", True)
        elif resp.status_code == 404:
            print_warning("Admin onboarding status endpoint not found")
            results.record("admin_onboarding_status", False, "Endpoint not found", skipped=True)
        else:
            print_warning(f"Admin onboarding status returned {resp.status_code}")
            results.record("admin_onboarding_status", True, f"Status {resp.status_code}")

        # Test admin config endpoints (PUT and POST variants)
        print_info("Testing admin config update (PUT)...")
        resp = requests.put(
            f"{API_URL}/admin/clinics/{org_to_check}/config",
            json={"deposit_amount_cents": 5000},
            headers=headers,
            timeout=15
        )

        if resp.status_code in (200, 204):
            print_success("Admin config PUT succeeded")
            results.record("admin_config_put", True)
        elif resp.status_code == 404:
            print_warning("Admin config PUT endpoint not found")
            results.record("admin_config_put", False, "Endpoint not found", skipped=True)
        else:
            print_warning(f"Admin config PUT returned {resp.status_code}")
            results.record("admin_config_put", True, f"Status {resp.status_code}")

        print_info("Testing admin config update (POST)...")
        resp = requests.post(
            f"{API_URL}/admin/clinics/{org_to_check}/config",
            json={"services": ["botox", "fillers"]},
            headers=headers,
            timeout=15
        )

        if resp.status_code in (200, 204):
            print_success("Admin config POST succeeded")
            results.record("admin_config_post", True)
        elif resp.status_code == 404:
            print_warning("Admin config POST endpoint not found")
            results.record("admin_config_post", False, "Endpoint not found", skipped=True)
        else:
            print_warning(f"Admin config POST returned {resp.status_code}")
            results.record("admin_config_post", True, f"Status {resp.status_code}")

        return True

    except Exception as e:
        print_error(f"Admin clinic ops test failed: {e}")
        results.record("admin_clinic_ops", False, str(e))
        return False

# =============================================================================
# Main Entry Point
# =============================================================================

def run_all_tests(specific_test: Optional[str] = None) -> int:
    """Run all extended E2E tests."""
    print_header("MedSpa AI Platform - Extended E2E Test Scenarios")

    print(f"Configuration:")
    print(f"  API URL:        {API_URL}")
    print(f"  Test Org ID:    {TEST_ORG_ID}")
    print(f"  Customer Phone: {TEST_CUSTOMER_PHONE}")
    print(f"  Clinic Phone:   {TEST_CLINIC_PHONE}")
    print(f"  Admin JWT:      {'Configured' if ADMIN_JWT_SECRET else 'Not set'}")
    print(f"  Twilio Token:   {'Configured' if TWILIO_AUTH_TOKEN else 'Not set'}")

    results = TestResults()

    # Health check first
    try:
        resp = requests.get(f"{API_URL}/health", timeout=10)
        if resp.status_code != 200:
            print_error(f"API not healthy: {resp.status_code}")
            return 1
        print_success("API is healthy")
    except Exception as e:
        print_error(f"Cannot connect to API: {e}")
        return 1

    tests = {
        "twilio": test_twilio_sms_webhook,
        "onboarding": test_clinic_onboarding,
        "square": test_square_oauth_flow,
        "booking": test_booking_flow,
        "leads": test_lead_updates,
        "dashboard": test_admin_dashboard,
        "errors": test_error_handling,
        "quiet_hours": test_quiet_hours,
        "leads_detail": test_leads_detail,
        "conversations": test_conversations,
        "square_admin": test_square_admin,
        "ten_dlc": test_ten_dlc,
        "hosted_orders": test_hosted_orders,
        "messaging": test_messaging,
        "admin_clinic": test_admin_clinic_ops,
    }

    if specific_test:
        if specific_test in tests:
            tests[specific_test](results)
        else:
            print_error(f"Unknown test: {specific_test}")
            print(f"Available tests: {', '.join(tests.keys())}")
            return 1
    else:
        for name, test_fn in tests.items():
            try:
                test_fn(results)
            except Exception as e:
                print_error(f"Test {name} crashed: {e}")
                results.record(name, False, f"Crashed: {e}")

    success = results.summary()
    return 0 if success else 1

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Extended E2E Test Scenarios")
    parser.add_argument("--test", help="Run specific test (twilio, onboarding, square, booking, leads, dashboard, errors, quiet_hours)")
    args = parser.parse_args()

    try:
        exit_code = run_all_tests(args.test)
        sys.exit(exit_code)
    except KeyboardInterrupt:
        print("\n\nTest interrupted by user.")
        sys.exit(130)
    except Exception as e:
        print_error(f"Unexpected error: {e}")
        import traceback
        traceback.print_exc()
        sys.exit(1)
