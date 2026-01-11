#!/usr/bin/env python3
"""
Brilliant Aesthetics Video Demo

A polished demo video for Brilliant Aesthetics (Strongsville, OH) featuring:
- Enhanced iOS phone simulator with illustrated hand overlay
- iOS notification banners and incoming call UI
- Sound effects (ringing, whoosh, tri-tone, cha-ching)
- Simulated Square checkout flow inside phone
- Dashboard metrics overlay

Scenario: Missed call -> AI reaches out -> Customer inquires about weight loss (GLP-1)
         -> AI demonstrates knowledge -> Books with deposit -> Payment confirmation

Usage:
    python scripts/brilliant_aesthetics_demo.py

Environment Variables:
    API_URL - API endpoint (default: https://api-dev.aiwolfsolutions.com)
    ADMIN_JWT_SECRET - Admin JWT secret for authentication
    TEST_ORG_ID - Organization ID (default: test org)
"""

from __future__ import annotations

import argparse
import base64
import hashlib
import hmac
import json
import os
import re
import sys
import time
import traceback
import uuid
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Dict, List, Optional, Tuple
from urllib.parse import quote

_PROJECT_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(_PROJECT_ROOT / "scripts"))

# Constants
CLINIC_NAME = "Brilliant Aesthetics"
CLINIC_PHONE = os.getenv("TEST_CLINIC_PHONE", "+14407325929")
CLINIC_AVATAR = "✨"

# Timing settings (can be adjusted via env vars)
def message_delay() -> float:
    return float(os.getenv("DEMO_MESSAGE_DELAY", "2.5"))

def reading_delay() -> float:
    return float(os.getenv("DEMO_READING_DELAY", "3.0"))

def ai_wait_timeout() -> float:
    return float(os.getenv("DEMO_AI_WAIT_TIMEOUT", "120"))

def http_timeout() -> float:
    return float(os.getenv("E2E_HTTP_TIMEOUT", "20"))


def _b64url(data: bytes) -> str:
    return base64.urlsafe_b64encode(data).rstrip(b"=").decode("ascii")


def make_admin_jwt(secret: str, *, ttl_seconds: int = 30 * 60) -> str:
    now = int(time.time())
    header = {"alg": "HS256", "typ": "JWT"}
    payload = {"iat": now, "exp": now + int(ttl_seconds), "role": "admin"}
    header_b64 = _b64url(json.dumps(header, separators=(",", ":")).encode("utf-8"))
    payload_b64 = _b64url(json.dumps(payload, separators=(",", ":")).encode("utf-8"))
    signing_input = f"{header_b64}.{payload_b64}".encode("ascii")
    sig = hmac.new(secret.encode("utf-8"), signing_input, hashlib.sha256).digest()
    return f"{header_b64}.{payload_b64}.{_b64url(sig)}"


def require_env(name: str) -> str:
    value = (os.getenv(name) or "").strip()
    if not value:
        raise RuntimeError(f"Missing required env var: {name}")
    return value


def mkdirp(path: Path) -> None:
    path.mkdir(parents=True, exist_ok=True)


def now_run_id() -> str:
    return time.strftime("%Y%m%d_%H%M%S")


@dataclass(frozen=True)
class TranscriptMessage:
    id: str
    role: str
    body: str
    kind: str
    timestamp: str
    metadata: Dict[str, str]


def _parse_transcript_messages(payload: Dict[str, Any]) -> List[TranscriptMessage]:
    raw = payload.get("messages") or []
    out: List[TranscriptMessage] = []
    for m in raw:
        if not isinstance(m, dict):
            continue
        out.append(
            TranscriptMessage(
                id=str(m.get("id") or ""),
                role=str(m.get("role") or ""),
                body=str(m.get("body") or ""),
                kind=str(m.get("kind") or ""),
                timestamp=str(m.get("timestamp") or ""),
                metadata=m.get("metadata") or {},
            )
        )
    return out


class BrilliantDemo:
    """Runs the Brilliant Aesthetics demo with enhanced phone simulator."""

    def __init__(self, api_url: str, token: str, artifacts_dir: Path):
        self.api_url = api_url.rstrip("/")
        self.token = token
        self.artifacts_dir = artifacts_dir
        self.org_id = os.getenv("TEST_ORG_ID", "bb507f20-7fcc-4941-9eac-9ed93b7834ed")
        self.clinic_phone = CLINIC_PHONE
        self.customer_phone = os.getenv("DEMO_PHONE", "+15550002001")
        self._requests = None

    @property
    def requests(self):
        if self._requests is None:
            import requests
            self._requests = requests
        return self._requests

    def admin_headers(self) -> Dict[str, str]:
        return {"Authorization": f"Bearer {self.token}", "Content-Type": "application/json"}

    def get_transcript(self, phone: str) -> Tuple[str, List[TranscriptMessage]]:
        url = f"{self.api_url}/admin/clinics/{quote(self.org_id)}/sms/{quote(phone, safe='')}" + "?limit=500"
        resp = self.requests.get(url, headers=self.admin_headers(), timeout=http_timeout())
        if resp.status_code != 200:
            raise RuntimeError(f"GET {url} failed: {resp.status_code} {resp.text[:300]}")
        data = resp.json() or {}
        conversation_id = str(data.get("conversation_id") or "")
        return conversation_id, _parse_transcript_messages(data)

    def purge_phone(self, phone: str) -> None:
        url = f"{self.api_url}/admin/clinics/{quote(self.org_id)}/phones/{quote(phone, safe='')}"
        resp = self.requests.delete(url, headers=self.admin_headers(), timeout=http_timeout())
        if resp.status_code not in (200, 204, 404):
            print(f"Warning: purge {phone} returned {resp.status_code}")

    def get_dashboard_metrics(self) -> Dict[str, Any]:
        url = f"{self.api_url}/admin/orgs/{quote(self.org_id)}/dashboard"
        resp = self.requests.get(url, headers=self.admin_headers(), timeout=http_timeout())
        if resp.status_code != 200:
            return {"leads": {}, "conversations": {}, "payments": {}}
        return resp.json() or {}

    def send_telnyx_voice_webhook(self, phone: str, *, hangup_cause: str = "no_answer") -> bool:
        import e2e_full_flow as base
        return base.send_telnyx_voice_webhook(
            hangup_cause,
            from_phone=phone,
            to_phone=self.clinic_phone,
        )

    def send_telnyx_sms_webhook(self, phone: str, message: str) -> bool:
        import e2e_full_flow as base
        return base.send_telnyx_sms_webhook(
            message,
            from_phone=phone,
            to_phone=self.clinic_phone,
        )

    def send_square_payment_webhook(self, lead_id: str, payment_id: str, amount_cents: int) -> bool:
        import e2e_full_flow as base
        return base.send_square_payment_webhook(lead_id, payment_id, amount_cents)

    def wait_for_message(
        self,
        phone: str,
        *,
        since_ids: List[str],
        kind: Optional[str] = None,
        role: str = "assistant",
        timeout_s: float = 60.0,
    ) -> Optional[TranscriptMessage]:
        deadline = time.time() + timeout_s
        since = set(since_ids)
        while time.time() < deadline:
            _, msgs = self.get_transcript(phone)
            for m in msgs:
                if m.id and m.id in since:
                    continue
                if role and m.role != role:
                    continue
                if kind and m.kind != kind:
                    continue
                return m
            time.sleep(0.6)
        return None

    def get_recent_pending_payment(self) -> Optional[Tuple[str, str]]:
        import e2e_full_flow as base
        try:
            sql = f"SELECT lead_id, id FROM payments WHERE org_id = '{self.org_id}' AND status = 'deposit_pending' ORDER BY created_at DESC LIMIT 1;"
            result = base.run_psql(sql, tuples_only=True, timeout=10)
            if result is None or result.returncode != 0:
                return None
            output = result.stdout.strip()
            if not output:
                return None
            parts = output.split("|")
            if len(parts) >= 2:
                return (parts[0].strip(), parts[1].strip())
            return None
        except Exception:
            return None


# Simulated Square checkout HTML for in-phone display
CHECKOUT_HTML = """
<!DOCTYPE html>
<html>
<head>
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <style>
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body { font-family: -apple-system, BlinkMacSystemFont, sans-serif; background: #f5f5f5; }
        .header { background: #006aff; color: white; padding: 16px; text-align: center; }
        .header svg { height: 28px; fill: white; }
        .header h1 { font-size: 14px; font-weight: 500; margin-top: 8px; }
        .content { padding: 20px; }
        .amount { background: white; border-radius: 12px; padding: 24px; text-align: center; margin-bottom: 20px; box-shadow: 0 2px 8px rgba(0,0,0,0.08); }
        .amount .label { font-size: 14px; color: #666; margin-bottom: 8px; }
        .amount .value { font-size: 36px; font-weight: 700; color: #1a1a1a; }
        .card { background: white; border-radius: 12px; padding: 20px; box-shadow: 0 2px 8px rgba(0,0,0,0.08); }
        .card h2 { font-size: 16px; margin-bottom: 16px; color: #1a1a1a; }
        .field { margin-bottom: 16px; }
        .field label { display: block; font-size: 12px; color: #666; margin-bottom: 6px; }
        .field input { width: 100%; padding: 14px; border: 1px solid #ddd; border-radius: 8px; font-size: 16px; background: #fafafa; }
        .row { display: flex; gap: 12px; }
        .row .field { flex: 1; }
        .btn { width: 100%; background: #006aff; color: white; border: none; padding: 16px; border-radius: 8px; font-size: 16px; font-weight: 600; margin-top: 20px; cursor: pointer; transition: background 0.2s; }
        .btn:active { background: #0052cc; }
        .secure { display: flex; align-items: center; justify-content: center; gap: 6px; margin-top: 16px; font-size: 12px; color: #666; }
        .processing { display: none; flex-direction: column; align-items: center; padding: 60px 20px; }
        .processing.show { display: flex; }
        .processing .spinner { width: 40px; height: 40px; border: 3px solid #ddd; border-top-color: #006aff; border-radius: 50%; animation: spin 1s linear infinite; }
        @keyframes spin { to { transform: rotate(360deg); } }
        .processing .text { margin-top: 16px; font-size: 16px; color: #333; }
        .form { display: block; }
        .form.hide { display: none; }
    </style>
</head>
<body>
    <div class="header">
        <svg viewBox="0 0 56 56"><path d="M28 0C12.536 0 0 12.536 0 28s12.536 28 28 28 28-12.536 28-28S43.464 0 28 0zm12.25 30.625H30.625v9.625c0 1.449-1.176 2.625-2.625 2.625s-2.625-1.176-2.625-2.625v-9.625H15.75c-1.449 0-2.625-1.176-2.625-2.625s1.176-2.625 2.625-2.625h9.625V15.75c0-1.449 1.176-2.625 2.625-2.625s2.625 1.176 2.625 2.625v9.625h9.625c1.449 0 2.625 1.176 2.625 2.625s-1.176 2.625-2.625 2.625z"/></svg>
        <h1>Brilliant Aesthetics</h1>
    </div>
    <div class="content">
        <div class="form" id="form">
            <div class="amount">
                <div class="label">Appointment Deposit</div>
                <div class="value">$50.00</div>
            </div>
            <div class="card">
                <h2>Payment Details</h2>
                <div class="field">
                    <label>Card Number</label>
                    <input type="text" id="cardNum" placeholder="1234 5678 9012 3456" maxlength="19">
                </div>
                <div class="row">
                    <div class="field">
                        <label>Expiry</label>
                        <input type="text" id="expiry" placeholder="MM/YY" maxlength="5">
                    </div>
                    <div class="field">
                        <label>CVV</label>
                        <input type="text" id="cvv" placeholder="123" maxlength="4">
                    </div>
                </div>
                <button class="btn" id="payBtn" onclick="processPayment()">Pay $50.00</button>
                <div class="secure">
                    <svg width="14" height="14" fill="#666" viewBox="0 0 24 24"><path d="M18 8h-1V6c0-2.76-2.24-5-5-5S7 3.24 7 6v2H6c-1.1 0-2 .9-2 2v10c0 1.1.9 2 2 2h12c1.1 0 2-.9 2-2V10c0-1.1-.9-2-2-2zm-6 9c-1.1 0-2-.9-2-2s.9-2 2-2 2 .9 2 2-.9 2-2 2zm3.1-9H8.9V6c0-1.71 1.39-3.1 3.1-3.1 1.71 0 3.1 1.39 3.1 3.1v2z"/></svg>
                    Secured by Square
                </div>
            </div>
        </div>
        <div class="processing" id="processing">
            <div class="spinner"></div>
            <div class="text">Processing payment...</div>
        </div>
    </div>
    <script>
        // Auto-format card number
        document.getElementById('cardNum').addEventListener('input', function(e) {
            let val = e.target.value.replace(/\\D/g, '').substring(0, 16);
            val = val.replace(/(\\d{4})(?=\\d)/g, '$1 ');
            e.target.value = val;
        });
        // Auto-format expiry
        document.getElementById('expiry').addEventListener('input', function(e) {
            let val = e.target.value.replace(/\\D/g, '').substring(0, 4);
            if (val.length > 2) val = val.substring(0,2) + '/' + val.substring(2);
            e.target.value = val;
        });
        function processPayment() {
            document.getElementById('form').classList.add('hide');
            document.getElementById('processing').classList.add('show');
            // Parent window will handle the actual completion
            if (window.parent && window.parent.onPaymentProcessing) {
                window.parent.onPaymentProcessing();
            }
        }
    </script>
</body>
</html>
"""


def run_demo(runner: BrilliantDemo, page):
    """
    Run the main demo scenario for Brilliant Aesthetics.

    Scenario: Weight Loss Inquiry
    - Patient missed a call from the clinic
    - AI proactively reaches out
    - Patient asks about weight loss program (GLP-1)
    - AI demonstrates expert knowledge
    - Patient books consultation with deposit
    - Payment confirmation
    """
    phone = runner.customer_phone
    print(f"\n{'='*60}")
    print(f"  BRILLIANT AESTHETICS DEMO")
    print(f"  Scenario: Weight Loss Inquiry → Booking")
    print(f"  Phone: {phone}")
    print(f"{'='*60}")

    # Purge previous data and get initial state
    runner.purge_phone(phone)
    time.sleep(1)
    _, msgs = runner.get_transcript(phone)
    ids = [m.id for m in msgs if m.id]

    # =========================================================================
    # Step 1: Missed Call - Show full iOS incoming call UI
    # =========================================================================
    print("\n  [Step 1] MISSED CALL")
    print("           Patient's phone rings... (4 rings)")

    # Get phone position for hand animation
    phone_bounds = page.locator(".phone").bounding_box()
    phone_center_x = phone_bounds["x"] + phone_bounds["width"] / 2
    phone_center_y = phone_bounds["y"] + phone_bounds["height"] / 2

    # Show incoming call with full iOS UI
    page.evaluate(f"window.showIncomingCall('{CLINIC_NAME}', '{CLINIC_AVATAR}')")
    time.sleep(6)  # Let it ring 3 times

    # Show hand swiping to decline (patient misses call)
    decline_btn_y = phone_center_y + 280
    page.evaluate(f"window.showHand({phone_center_x - 60}, {decline_btn_y})")
    time.sleep(0.8)
    page.evaluate("window.endCall('no answer')")
    time.sleep(1.5)
    page.evaluate("window.hideHand()")

    # Trigger voice webhook for missed call
    runner.send_telnyx_voice_webhook(phone, hangup_cause="no_answer")

    # Wait for AI's proactive outreach message
    ack = runner.wait_for_message(phone, since_ids=ids, kind="voice_ack", timeout_s=30)
    if ack:
        # Show notification banner when AI message arrives
        preview = ack.body[:50] + "..." if len(ack.body) > 50 else ack.body
        page.evaluate(f"window.showNotification('{CLINIC_NAME}', '{preview}', 4000)")
        print(f"           AI: \"{ack.body[:80]}...\"")
    time.sleep(message_delay())

    # =========================================================================
    # Step 2: Customer reads message and inquires about weight loss
    # =========================================================================
    _, msgs = runner.get_transcript(phone)
    ids = [m.id for m in msgs if m.id]

    print("\n  [Step 2] WEIGHT LOSS INQUIRY")
    print("           (Customer reads the message...)")
    time.sleep(reading_delay())

    # Show hand tapping on message input
    input_y = phone_bounds["y"] + phone_bounds["height"] - 50
    page.evaluate(f"window.tapHand({phone_center_x}, {input_y})")
    time.sleep(0.5)

    inquiry = "Hi! I've been seeing ads about weight loss shots. Do you offer that? How does it work?"
    print(f"           Customer: \"{inquiry}\"")

    # Play sent sound and hide hand
    page.evaluate("window.playSentSound()")
    page.evaluate("window.hideHand()")
    runner.send_telnyx_sms_webhook(phone, inquiry)

    # Show typing indicator while waiting
    page.evaluate("window.showTyping()")
    reply1 = runner.wait_for_message(phone, since_ids=ids, kind="ai_reply", timeout_s=ai_wait_timeout())
    page.evaluate("window.hideTyping()")

    if reply1:
        # Play received notification sound
        time.sleep(0.3)
        page.evaluate("window.playTriTone()")
        print(f"           AI: \"{reply1.body[:100]}...\"")
    time.sleep(message_delay())

    # =========================================================================
    # Step 3: Customer asks about pricing and timeline
    # =========================================================================
    _, msgs = runner.get_transcript(phone)
    ids = [m.id for m in msgs if m.id]

    print("\n  [Step 3] PRICING QUESTION")
    print("           (Customer reads the detailed response...)")
    time.sleep(reading_delay())

    page.evaluate(f"window.tapHand({phone_center_x}, {input_y})")
    time.sleep(0.5)

    pricing_q = "That sounds perfect! How much does it cost per month and how quickly can I get started?"
    print(f"           Customer: \"{pricing_q}\"")

    page.evaluate("window.playSentSound()")
    page.evaluate("window.hideHand()")
    runner.send_telnyx_sms_webhook(phone, pricing_q)

    page.evaluate("window.showTyping()")
    reply2 = runner.wait_for_message(phone, since_ids=ids, kind="ai_reply", timeout_s=ai_wait_timeout())
    page.evaluate("window.hideTyping()")

    if reply2:
        time.sleep(0.3)
        page.evaluate("window.playTriTone()")
        print(f"           AI: \"{reply2.body[:100]}...\"")
    time.sleep(message_delay())

    # =========================================================================
    # Step 4: Customer is ready to book
    # =========================================================================
    _, msgs = runner.get_transcript(phone)
    ids = [m.id for m in msgs if m.id]

    print("\n  [Step 4] BOOKING DECISION")
    print("           (Customer is convinced...)")
    time.sleep(reading_delay())

    page.evaluate(f"window.tapHand({phone_center_x}, {input_y})")
    time.sleep(0.5)

    booking_msg = "I'm ready to book! I'm Jennifer. Do you have anything available this week? Happy to pay the deposit."
    print(f"           Customer: \"{booking_msg}\"")

    page.evaluate("window.playSentSound()")
    page.evaluate("window.hideHand()")
    runner.send_telnyx_sms_webhook(phone, booking_msg)

    # Wait for AI reply
    page.evaluate("window.showTyping()")
    ai_reply = runner.wait_for_message(phone, since_ids=ids, kind="ai_reply", timeout_s=ai_wait_timeout())
    page.evaluate("window.hideTyping()")

    if ai_reply:
        time.sleep(0.3)
        page.evaluate("window.playTriTone()")
        print(f"           AI: \"{ai_reply.body[:100]}...\"")

    # Wait for deposit link
    deposit_link = runner.wait_for_message(phone, since_ids=ids, kind="deposit_link", timeout_s=30)
    if deposit_link:
        time.sleep(0.3)
        page.evaluate("window.playTriTone()")
        print(f"           DEPOSIT LINK: \"{deposit_link.body[:70]}...\"")
    else:
        # Search for Square link in messages
        _, msgs = runner.get_transcript(phone)
        for m in msgs:
            if m.id not in ids and "checkout.square" in m.body.lower():
                deposit_link = m
                break
    time.sleep(message_delay())

    # =========================================================================
    # Step 5: Customer opens checkout and pays
    # =========================================================================
    print("\n  [Step 5] PAYMENT")

    checkout_url = None
    if deposit_link:
        url_match = re.search(r'https?://[^\s]+', deposit_link.body)
        if url_match:
            checkout_url = url_match.group(0)

    _, msgs = runner.get_transcript(phone)
    ids_before_payment = [m.id for m in msgs if m.id]

    if checkout_url:
        print(f"           Customer taps payment link...")

        # Show hand tapping on the link
        messages_area_y = phone_center_y + 100
        page.evaluate(f"window.tapHand({phone_center_x}, {messages_area_y})")
        time.sleep(0.6)
        page.evaluate("window.hideHand()")

        # Open simulated checkout inside phone browser
        checkout_b64 = base64.b64encode(CHECKOUT_HTML.encode()).decode()
        page.evaluate(f"window.openBrowser('data:text/html;base64,{checkout_b64}')")
        print("           [Checkout page opens in phone]")
        time.sleep(2)

        # Simulate customer entering card details
        print("           Customer enters card details...")

        # Show hand typing card number
        card_input_y = phone_center_y - 20
        page.evaluate(f"window.showHand({phone_center_x}, {card_input_y})")
        time.sleep(1.5)

        # Fill in card number via JavaScript
        try:
            frame = page.frame_locator("#browserFrame")
            frame.locator("#cardNum").fill("4532 8721 3456 7890")
            time.sleep(0.5)
            frame.locator("#expiry").fill("12/27")
            time.sleep(0.3)
            frame.locator("#cvv").fill("123")
            time.sleep(0.5)
        except Exception as e:
            print(f"           (Card input automation skipped: {e})")

        # Tap pay button
        pay_btn_y = phone_center_y + 160
        page.evaluate(f"window.tapHand({phone_center_x}, {pay_btn_y})")
        time.sleep(0.5)

        try:
            frame.locator("#payBtn").click()
            print("           Customer taps 'Pay $50.00'")
        except Exception:
            pass

        page.evaluate("window.hideHand()")
        time.sleep(2)

        # Close browser and show payment success overlay
        page.evaluate("window.closeBrowser()")
        page.evaluate("window.showPaymentSuccess('$50.00 deposit confirmed')")
        print("           PAYMENT COMPLETE!")
        time.sleep(2.5)
        page.evaluate("window.hidePaymentSuccess()")

        # Send actual payment webhook
        payment_info = None
        if deposit_link and deposit_link.metadata:
            payment_id = deposit_link.metadata.get("payment_id")
            lead_id = deposit_link.metadata.get("lead_id")
            if payment_id and lead_id:
                payment_info = (lead_id, payment_id)
        if not payment_info:
            payment_info = runner.get_recent_pending_payment()

        if payment_info:
            lead_id, payment_id = payment_info
            runner.send_square_payment_webhook(lead_id, payment_id, 5000)

        time.sleep(3)

    # =========================================================================
    # Step 6: Confirmation SMS
    # =========================================================================
    print("\n  [Step 6] CONFIRMATION")
    print("           Waiting for confirmation SMS...")

    confirm = runner.wait_for_message(
        phone, since_ids=ids_before_payment, kind="payment_confirmation", timeout_s=60
    )
    if confirm:
        # Show notification banner for confirmation
        page.evaluate(f"window.showNotification('{CLINIC_NAME}', 'Payment confirmed! ✓', 5000)")
        page.evaluate("window.playTriTone()")
        print(f"           CONFIRMATION: \"{confirm.body[:90]}...\"")
        time.sleep(3)

    # Final pause to show completed conversation
    time.sleep(3)

    print(f"\n  {'='*58}")
    print("  DEMO COMPLETE!")
    print("  - Missed call → AI reached out proactively")
    print("  - Weight loss inquiry → AI demonstrated expertise")
    print("  - Customer booked → Deposit collected")
    print("  - Payment confirmed → Ready for appointment")
    print(f"  {'='*58}")

    return {"success": True, "payment_confirmed": confirm is not None}


def update_dashboard_overlay(runner: BrilliantDemo, page):
    """Update the metrics overlay on the page."""
    try:
        metrics = runner.get_dashboard_metrics()
        conversations = metrics.get("conversations", {}).get("unique_conversations", 0)
        deposits = metrics.get("payments", {}).get("total_collected_cents", 0)
        conversion_rate = metrics.get("leads", {}).get("conversion_rate", 0)
        page.evaluate(f"window.updateMetrics && window.updateMetrics({conversations}, {deposits}, {conversion_rate})")
    except Exception:
        pass


# Dashboard metrics overlay JavaScript
METRICS_OVERLAY_JS = """
(function() {
    const overlay = document.createElement('div');
    overlay.id = 'metricsOverlay';
    overlay.innerHTML = `
        <style>
            #metricsOverlay {
                position: fixed;
                top: 20px;
                right: 20px;
                background: rgba(0,0,0,0.85);
                border-radius: 16px;
                padding: 16px 24px;
                font-family: -apple-system, BlinkMacSystemFont, sans-serif;
                color: white;
                z-index: 10000;
                box-shadow: 0 8px 32px rgba(0,0,0,0.4);
                border: 1px solid rgba(255,255,255,0.1);
            }
            #metricsOverlay .title {
                font-size: 12px;
                font-weight: 600;
                color: rgba(255,255,255,0.6);
                margin-bottom: 12px;
                text-transform: uppercase;
                letter-spacing: 1px;
            }
            #metricsOverlay .metrics {
                display: flex;
                gap: 24px;
            }
            #metricsOverlay .metric {
                text-align: center;
            }
            #metricsOverlay .metric-value {
                font-size: 28px;
                font-weight: 700;
                color: #e94560;
            }
            #metricsOverlay .metric-label {
                font-size: 10px;
                color: rgba(255,255,255,0.5);
                text-transform: uppercase;
            }
            #metricsOverlay .branding {
                margin-top: 12px;
                padding-top: 10px;
                border-top: 1px solid rgba(255,255,255,0.1);
                font-size: 11px;
                color: rgba(255,255,255,0.4);
                text-align: center;
            }
        </style>
        <div class="title">Dashboard Metrics</div>
        <div class="metrics">
            <div class="metric">
                <div class="metric-value" id="valConversations">0</div>
                <div class="metric-label">Conversations</div>
            </div>
            <div class="metric">
                <div class="metric-value" id="valDeposits">$0</div>
                <div class="metric-label">Deposits</div>
            </div>
            <div class="metric">
                <div class="metric-value" id="valConversion">0%</div>
                <div class="metric-label">Conversion</div>
            </div>
        </div>
        <div class="branding">Brilliant Aesthetics • AI Receptionist</div>
    `;
    document.body.appendChild(overlay);

    window.updateMetrics = function(conversations, depositsCents, conversionRate) {
        const valConv = document.getElementById('valConversations');
        const valDep = document.getElementById('valDeposits');
        const valRate = document.getElementById('valConversion');
        if (valConv) valConv.textContent = String(conversations);
        if (valDep) valDep.textContent = '$' + (depositsCents / 100).toFixed(0);
        if (valRate) valRate.textContent = conversionRate.toFixed(0) + '%';
    };
})();
"""


def main() -> int:
    parser = argparse.ArgumentParser(description="Brilliant Aesthetics Video Demo")
    parser.add_argument("--api-url", default=os.getenv("API_URL", "https://api-dev.aiwolfsolutions.com"))
    parser.add_argument("--headed", action="store_true", help="Run Playwright headed (debug)")
    args = parser.parse_args()

    api_url = args.api_url.rstrip("/")
    os.environ["API_URL"] = api_url

    # Change to project root
    os.chdir(_PROJECT_ROOT)

    run_id = now_run_id()
    videos_dir = _PROJECT_ROOT / "tmp" / "brilliant_demo_videos"
    artifacts_dir = _PROJECT_ROOT / "tmp" / "brilliant_demo_artifacts" / run_id
    mkdirp(videos_dir)
    mkdirp(artifacts_dir)

    # Set knowledge file to Brilliant Aesthetics BEFORE importing e2e_full_flow
    # (that module reads env vars at import time)
    os.environ["KNOWLEDGE_FILE"] = "testdata/brilliant-aesthetics-knowledge.json"
    if "KNOWLEDGE_SCRAPE_URL" in os.environ:
        del os.environ["KNOWLEDGE_SCRAPE_URL"]

    # Load e2e module for webhook functions
    import e2e_full_flow as base

    # IMPORTANT: Override module-level constant that may have been cached with old env value
    base.KNOWLEDGE_SCRAPE_URL = ""

    # Required env vars
    admin_secret = require_env("ADMIN_JWT_SECRET")

    # Set default webhook secret if not set
    if not os.getenv("TELNYX_WEBHOOK_SECRET"):
        os.environ["TELNYX_WEBHOOK_SECRET"] = "demo-test-secret"

    sms_provider = (os.getenv("SMS_PROVIDER") or "").strip().lower()
    if sms_provider != "telnyx":
        os.environ["SMS_PROVIDER"] = "telnyx"
        print("  Note: Set SMS_PROVIDER=telnyx")

    token = make_admin_jwt(admin_secret)
    runner = BrilliantDemo(api_url, token, artifacts_dir)

    # Check API health
    if not base.check_health():
        raise RuntimeError(f"API not healthy at {api_url}")

    # Seed Brilliant Aesthetics knowledge
    print("\n  Seeding Brilliant Aesthetics knowledge base...")
    if not base.seed_knowledge():
        raise RuntimeError("Knowledge seeding failed")

    # Configure clinic
    base.seed_hosted_number()
    base.seed_clinic_config(CLINIC_NAME)

    print(f"\n{'='*60}")
    print("  Brilliant Aesthetics - AI Receptionist Demo")
    print(f"{'='*60}")
    print(f"  API URL:    {api_url}")
    print(f"  Org ID:     {runner.org_id}")
    print(f"  Clinic:     {CLINIC_NAME}")
    print(f"  Output:     {videos_dir}/")
    print(f"{'='*60}\n")

    # Start Playwright
    from playwright.sync_api import sync_playwright

    video_path: Optional[str] = None

    with sync_playwright() as p:
        # Try multiple browsers as fallback on Windows
        browser = None
        for browser_name, launch_fn in [
            ("Edge", lambda: p.chromium.launch(channel="msedge", headless=not args.headed)),
            ("Chromium", lambda: p.chromium.launch(headless=not args.headed)),
            ("Firefox", lambda: p.firefox.launch(headless=not args.headed)),
        ]:
            try:
                print(f"  Launching {browser_name}...")
                browser = launch_fn()
                print(f"  {browser_name} launched successfully!")
                break
            except Exception as e:
                print(f"  {browser_name} failed: {str(e)[:100]}")
                continue
        if browser is None:
            raise RuntimeError("Could not launch any browser")
        context = browser.new_context(
            viewport={"width": 1280, "height": 900},
            record_video_dir=str(videos_dir),
            record_video_size={"width": 1280, "height": 900},
            extra_http_headers={"Authorization": f"Bearer {token}"},
        )
        page = context.new_page()

        try:
            # Navigate to enhanced phone simulator
            phone_url = (
                f"{api_url}/admin/e2e/phone-simulator-demo"
                f"?orgID={quote(runner.org_id)}"
                f"&phone={quote(runner.customer_phone, safe='')}"
                f"&clinic={quote(runner.clinic_phone, safe='')}"
                f"&clinic_name={quote(CLINIC_NAME)}"
                f"&poll_ms=600"
            )
            page.goto(phone_url, wait_until="networkidle")
            time.sleep(1)

            # Inject metrics overlay
            page.evaluate(METRICS_OVERLAY_JS)
            update_dashboard_overlay(runner, page)
            time.sleep(2)

            # Run the demo
            result = run_demo(runner, page)

            # Update final metrics
            update_dashboard_overlay(runner, page)
            time.sleep(5)

        except Exception:
            page.screenshot(path=str(artifacts_dir / "failure.png"), full_page=True)
            (artifacts_dir / "error.txt").write_text(traceback.format_exc(), encoding="utf-8")
            raise
        finally:
            page.close()
            context.close()
            video = page.video
            if video:
                video_path = video.path()
            browser.close()

    print(f"\n{'='*60}")
    print("  Demo Recording Complete")
    print(f"{'='*60}")
    if video_path:
        print(f"  Video: {video_path}")
    print(f"  Artifacts: {artifacts_dir}/")
    print(f"{'='*60}\n")

    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except KeyboardInterrupt:
        raise SystemExit(130)
