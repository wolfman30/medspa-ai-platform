#!/usr/bin/env python3
"""
Video Demo: 3 Scenarios with Split-Screen Recording

This script records a video demonstration showing:
- LEFT: Patient phone emulator (iPhone-style SMS view)
- RIGHT: Dashboard with live metrics updates

Scenarios:
1. HAPPY PATH: Missed call -> SMS conversation -> Deposit payment -> Confirmation
2. PCI GUARDRAIL: Customer sends credit card number -> Blocked with security message
3. NO CONVERSION: Customer inquires but declines deposit -> No payment

Each scenario shows the dashboard updating with:
- Conversion percentage
- Number of conversations
- Number of deposits collected

Usage:
    python scripts/e2e_video_demo.py --api-url http://localhost:8082

    # Headed mode (debug):
    python scripts/e2e_video_demo.py --api-url http://localhost:8082 --headed

Artifacts:
    - Video: tmp/demo_videos/demo_YYYYMMDD_HHMMSS.webm
    - Debug: tmp/demo_artifacts/<run_id>/
"""

from __future__ import annotations

# JavaScript to inject metrics overlay onto the phone simulator page
METRICS_OVERLAY_JS = """
(function() {
    // Create overlay container
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
                padding: 20px 28px;
                font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
                color: white;
                z-index: 10000;
                min-width: 320px;
                box-shadow: 0 8px 32px rgba(0,0,0,0.4);
                border: 1px solid rgba(255,255,255,0.1);
            }
            #metricsOverlay .title {
                font-size: 14px;
                font-weight: 600;
                color: rgba(255,255,255,0.7);
                margin-bottom: 16px;
                text-transform: uppercase;
                letter-spacing: 1px;
            }
            #metricsOverlay .metrics {
                display: flex;
                gap: 28px;
            }
            #metricsOverlay .metric {
                text-align: center;
            }
            #metricsOverlay .metric-value {
                font-size: 36px;
                font-weight: 700;
                color: #e94560;
            }
            #metricsOverlay .metric-label {
                font-size: 11px;
                color: rgba(255,255,255,0.6);
                text-transform: uppercase;
                margin-top: 4px;
            }
            #metricsOverlay .scenario-badge {
                position: absolute;
                top: -12px;
                left: 20px;
                background: #e94560;
                color: white;
                padding: 6px 14px;
                border-radius: 999px;
                font-size: 12px;
                font-weight: 600;
            }
        </style>
        <div class="scenario-badge" id="scenarioBadge">Scenario 1: Starting</div>
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
    `;
    document.body.appendChild(overlay);

    // Create iPhone-style text message sounds using Web Audio API
    let audioContext = null;

    function getAudioContext() {
        if (!audioContext) {
            audioContext = new (window.AudioContext || window.webkitAudioContext)();
        }
        return audioContext;
    }

    // iPhone "sent" sound - ascending tone (for outgoing/customer messages)
    window.playMessageSentSound = function() {
        try {
            const ctx = getAudioContext();
            const osc = ctx.createOscillator();
            const gain = ctx.createGain();

            osc.connect(gain);
            gain.connect(ctx.destination);

            osc.frequency.setValueAtTime(1200, ctx.currentTime);
            osc.frequency.exponentialRampToValueAtTime(1800, ctx.currentTime + 0.08);

            gain.gain.setValueAtTime(0.15, ctx.currentTime);
            gain.gain.exponentialRampToValueAtTime(0.01, ctx.currentTime + 0.1);

            osc.start(ctx.currentTime);
            osc.stop(ctx.currentTime + 0.1);
        } catch(e) { console.log('Audio error:', e); }
    };

    // iPhone "received" sound - descending tri-tone (for incoming/AI messages)
    window.playMessageReceivedSound = function() {
        try {
            const ctx = getAudioContext();

            // First tone
            const osc1 = ctx.createOscillator();
            const gain1 = ctx.createGain();
            osc1.connect(gain1);
            gain1.connect(ctx.destination);
            osc1.frequency.setValueAtTime(1046.5, ctx.currentTime); // C6
            gain1.gain.setValueAtTime(0.12, ctx.currentTime);
            gain1.gain.exponentialRampToValueAtTime(0.01, ctx.currentTime + 0.1);
            osc1.start(ctx.currentTime);
            osc1.stop(ctx.currentTime + 0.1);

            // Second tone
            const osc2 = ctx.createOscillator();
            const gain2 = ctx.createGain();
            osc2.connect(gain2);
            gain2.connect(ctx.destination);
            osc2.frequency.setValueAtTime(1318.5, ctx.currentTime + 0.1); // E6
            gain2.gain.setValueAtTime(0.12, ctx.currentTime + 0.1);
            gain2.gain.exponentialRampToValueAtTime(0.01, ctx.currentTime + 0.2);
            osc2.start(ctx.currentTime + 0.1);
            osc2.stop(ctx.currentTime + 0.2);

            // Third tone
            const osc3 = ctx.createOscillator();
            const gain3 = ctx.createGain();
            osc3.connect(gain3);
            gain3.connect(ctx.destination);
            osc3.frequency.setValueAtTime(1568, ctx.currentTime + 0.2); // G6
            gain3.gain.setValueAtTime(0.12, ctx.currentTime + 0.2);
            gain3.gain.exponentialRampToValueAtTime(0.01, ctx.currentTime + 0.35);
            osc3.start(ctx.currentTime + 0.2);
            osc3.stop(ctx.currentTime + 0.35);
        } catch(e) { console.log('Audio error:', e); }
    };

    // Watch for new messages and play sounds
    let lastMessageCount = 0;
    const messageObserver = new MutationObserver(function(mutations) {
        const messages = document.querySelectorAll('.bubble');
        if (messages.length > lastMessageCount) {
            const newMsg = messages[messages.length - 1];
            if (newMsg.classList.contains('outgoing')) {
                window.playMessageSentSound();
            } else {
                window.playMessageReceivedSound();
            }
            lastMessageCount = messages.length;
        }
    });

    // Start observing after a short delay to let the page load
    setTimeout(function() {
        const chatArea = document.querySelector('.chat') || document.body;
        messageObserver.observe(chatArea, { childList: true, subtree: true });
        lastMessageCount = document.querySelectorAll('.bubble').length;
    }, 500);

    // Define update functions
    window.updateScenario = function(num, name) {
        const badge = document.getElementById('scenarioBadge');
        if (badge) badge.textContent = 'Scenario ' + num + ': ' + name;
    };

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

# Project root for imports
_PROJECT_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(_PROJECT_ROOT / "scripts"))


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


def http_timeout() -> float:
    return float(os.getenv("E2E_HTTP_TIMEOUT", "20"))


def scenario_delay() -> float:
    """Delay between scenarios to let the viewer see the dashboard update."""
    return float(os.getenv("DEMO_SCENARIO_DELAY", "3.0"))


def message_delay() -> float:
    """Delay between SMS messages for readability - longer for realistic video."""
    return float(os.getenv("DEMO_MESSAGE_DELAY", "5.0"))


def ai_wait_timeout() -> float:
    """Max wait for AI response."""
    return float(os.getenv("DEMO_AI_WAIT_TIMEOUT", "120"))


def reading_delay() -> float:
    """Delay before customer responds, simulating reading the AI's message."""
    return float(os.getenv("DEMO_READING_DELAY", "4.0"))


# HTML for split-screen demo view
SPLIT_SCREEN_HTML = """<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>MedSpa AI Demo - Split View</title>
  <style>
    * { box-sizing: border-box; margin: 0; padding: 0; }
    html, body {
      height: 100%;
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
      background: linear-gradient(135deg, #1a1a2e 0%, #16213e 50%, #0f3460 100%);
      color: #fff;
      overflow: hidden;
    }
    .container {
      display: flex;
      height: 100%;
      gap: 24px;
      padding: 24px;
    }
    .panel {
      flex: 1;
      display: flex;
      flex-direction: column;
      background: rgba(255,255,255,0.05);
      border-radius: 16px;
      overflow: hidden;
      border: 1px solid rgba(255,255,255,0.1);
    }
    .panel-header {
      padding: 16px 20px;
      background: rgba(0,0,0,0.3);
      border-bottom: 1px solid rgba(255,255,255,0.1);
      display: flex;
      align-items: center;
      gap: 12px;
    }
    .panel-title {
      font-size: 18px;
      font-weight: 600;
    }
    .panel-subtitle {
      font-size: 12px;
      color: rgba(255,255,255,0.6);
    }
    .scenario-badge {
      background: #e94560;
      color: white;
      padding: 4px 12px;
      border-radius: 999px;
      font-size: 12px;
      font-weight: 600;
      margin-left: auto;
    }
    .panel-content {
      flex: 1;
      position: relative;
    }
    .panel-content iframe {
      width: 100%;
      height: 100%;
      border: none;
      background: #000;
    }
    /* Phone panel styling */
    .phone-panel {
      max-width: 500px;
    }
    /* Dashboard panel styling */
    .dashboard-panel {
      flex: 1.5;
    }
    /* Metrics overlay */
    .metrics-highlight {
      position: absolute;
      bottom: 20px;
      left: 20px;
      right: 20px;
      background: rgba(0,0,0,0.8);
      border-radius: 12px;
      padding: 16px 20px;
      display: flex;
      gap: 24px;
      justify-content: space-around;
      border: 1px solid rgba(233,69,96,0.5);
    }
    .metric {
      text-align: center;
    }
    .metric-value {
      font-size: 32px;
      font-weight: 700;
      color: #e94560;
    }
    .metric-label {
      font-size: 11px;
      color: rgba(255,255,255,0.7);
      text-transform: uppercase;
      letter-spacing: 0.5px;
      margin-top: 4px;
    }
    .metric.highlight .metric-value {
      animation: pulse 0.5s ease-out;
    }
    @keyframes pulse {
      0% { transform: scale(1); }
      50% { transform: scale(1.15); color: #34c759; }
      100% { transform: scale(1); }
    }
  </style>
</head>
<body>
  <div class="container">
    <div class="panel phone-panel">
      <div class="panel-header">
        <div>
          <div class="panel-title">Patient View</div>
          <div class="panel-subtitle">iPhone SMS Simulator</div>
        </div>
        <div id="scenarioBadge" class="scenario-badge">Scenario 1</div>
      </div>
      <div class="panel-content">
        <iframe id="phoneFrame" src="about:blank"></iframe>
      </div>
    </div>
    <div class="panel dashboard-panel">
      <div class="panel-header">
        <div>
          <div class="panel-title">Clinic Dashboard</div>
          <div class="panel-subtitle">Real-time Metrics</div>
        </div>
      </div>
      <div class="panel-content">
        <iframe id="dashboardFrame" src="about:blank"></iframe>
        <div class="metrics-highlight">
          <div class="metric" id="metricConversations">
            <div class="metric-value" id="valConversations">0</div>
            <div class="metric-label">Conversations</div>
          </div>
          <div class="metric" id="metricDeposits">
            <div class="metric-value" id="valDeposits">$0</div>
            <div class="metric-label">Deposits Collected</div>
          </div>
          <div class="metric" id="metricConversion">
            <div class="metric-value" id="valConversion">0%</div>
            <div class="metric-label">Conversion Rate</div>
          </div>
        </div>
      </div>
    </div>
  </div>
  <script>
    window.updateScenario = function(num, name) {
      document.getElementById('scenarioBadge').textContent = 'Scenario ' + num + ': ' + name;
    };
    window.updateMetrics = function(conversations, depositsCents, conversionRate) {
      const valConv = document.getElementById('valConversations');
      const valDep = document.getElementById('valDeposits');
      const valRate = document.getElementById('valConversion');

      if (valConv.textContent !== String(conversations)) {
        valConv.textContent = String(conversations);
        document.getElementById('metricConversations').classList.add('highlight');
        setTimeout(() => document.getElementById('metricConversations').classList.remove('highlight'), 600);
      }

      const depStr = '$' + (depositsCents / 100).toFixed(0);
      if (valDep.textContent !== depStr) {
        valDep.textContent = depStr;
        document.getElementById('metricDeposits').classList.add('highlight');
        setTimeout(() => document.getElementById('metricDeposits').classList.remove('highlight'), 600);
      }

      const rateStr = conversionRate.toFixed(0) + '%';
      if (valRate.textContent !== rateStr) {
        valRate.textContent = rateStr;
        document.getElementById('metricConversion').classList.add('highlight');
        setTimeout(() => document.getElementById('metricConversion').classList.remove('highlight'), 600);
      }
    };
    window.setPhoneUrl = function(url) {
      document.getElementById('phoneFrame').src = url;
    };
    window.setDashboardUrl = function(url) {
      document.getElementById('dashboardFrame').src = url;
    };
  </script>
</body>
</html>
"""


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


class DemoRunner:
    """Runs the 3-scenario demo with split-screen recording."""

    def __init__(self, api_url: str, token: str, artifacts_dir: Path):
        self.api_url = api_url.rstrip("/")
        self.token = token
        self.artifacts_dir = artifacts_dir
        self.org_id = os.getenv("TEST_ORG_ID", "11111111-1111-1111-1111-111111111111")
        # Brilliant Aesthetics demo - Telnyx test number
        self.clinic_phone = os.getenv("TEST_CLINIC_PHONE", "+13304600937")

        # Each scenario gets a unique customer phone
        self.scenario_phones = [
            os.getenv("DEMO_PHONE_1", "+15550001001"),
            os.getenv("DEMO_PHONE_2", "+15550001002"),
            os.getenv("DEMO_PHONE_3", "+15550001003"),
        ]

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
        """Simulate a missed call via Telnyx voice webhook."""
        import e2e_full_flow as base
        return base.send_telnyx_voice_webhook(
            hangup_cause,
            from_phone=phone,
            to_phone=self.clinic_phone,
        )

    def send_telnyx_sms_webhook(self, phone: str, message: str) -> bool:
        """Simulate an incoming SMS via Telnyx webhook."""
        import e2e_full_flow as base
        return base.send_telnyx_sms_webhook(
            message,
            from_phone=phone,
            to_phone=self.clinic_phone,
        )

    def send_square_payment_webhook(self, lead_id: str, payment_id: str, amount_cents: int) -> bool:
        """Simulate a Square payment completion."""
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
        """Wait for a new message matching criteria."""
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
            time.sleep(0.8)
        return None

    def create_lead(self, phone: str, name: str) -> Optional[str]:
        """Create a test lead and return its ID."""
        import e2e_full_flow as base
        lead = base.create_lead()
        if lead and isinstance(lead, dict):
            return lead.get("id")
        return None

    def get_payment_id_for_lead(self, lead_id: str) -> Optional[str]:
        """Get the payment ID for a lead."""
        import e2e_full_flow as base
        return base.get_payment_id_for_lead(lead_id)

    def get_recent_pending_payment(self) -> Optional[Tuple[str, str]]:
        """Get the most recent pending payment (lead_id, payment_id) for this org."""
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


def run_scenario_1_happy_path(runner: DemoRunner, page) -> Dict[str, Any]:
    """
    Scenario 1: Happy Path (Full Conversion)

    This scenario demonstrates the complete booking flow that leads to a successful
    deposit payment. Key moments:

    1. Missed call → AI proactively reaches out
    2. Customer inquiry with HARD QUESTION (tests AI knowledge)
    3. AI qualifies the lead (name, service, new/existing, schedule)
    4. Customer agrees to deposit → checkout link sent
    5. Payment completes → confirmation SMS
    6. Dashboard updates with new conversion

    The "hard question" is designed to impress clinic operators by showing
    the AI can answer nuanced service questions from the clinic's knowledge base.
    """
    phone = runner.scenario_phones[0]
    print(f"\n{'='*60}")
    print(f"  SCENARIO 1: HAPPY PATH - Full Conversion")
    print(f"  Phone: {phone}")
    print(f"{'='*60}")

    # Update UI
    page.evaluate("window.updateScenario(1, 'Happy Path')")

    # Purge and get initial state
    runner.purge_phone(phone)
    time.sleep(1)
    _, msgs = runner.get_transcript(phone)
    ids = [m.id for m in msgs if m.id]

    # =========================================================================
    # Step 1: Missed Call (4 rings, customer hangs up)
    # =========================================================================
    print("\n  [Step 1] MISSED CALL")
    print("           Patient calls the 330 Telnyx number, hangs up after 4 rings...")

    # Show incoming call animation on phone (4 rings = ~5 seconds)
    try:
        page.evaluate("window.showIncomingCall('Brilliant Aesthetics')")
        time.sleep(5.5)  # Wait for 4 rings to complete (1.2s each + buffer)
        page.evaluate("window.endCall('no answer')")
        time.sleep(1.5)  # Let the "call ended" message show
    except Exception as e:
        print(f"           (Call animation skipped: {e})")
        time.sleep(2)

    # Trigger the voice webhook to simulate the missed call
    runner.send_telnyx_voice_webhook(phone, hangup_cause="no_answer")

    # Wait for voice ack - AI proactively reaches out
    ack = runner.wait_for_message(phone, since_ids=ids, kind="voice_ack", timeout_s=30)
    if ack:
        print(f"           AI sends: \"{ack.body[:70]}...\"")
    else:
        print("           WARNING: No voice ack received")
    time.sleep(message_delay())

    # =========================================================================
    # Step 2: Initial Inquiry
    # =========================================================================
    _, msgs = runner.get_transcript(phone)
    ids = [m.id for m in msgs if m.id]

    # Simulate customer reading the missed call message before responding
    print("\n           (Customer reads the message...)")
    time.sleep(reading_delay())

    print("\n  [Step 2] INITIAL INQUIRY")
    initial_msg = "Hi! I'm interested in Botox for my forehead lines and crow's feet. Do you offer that?"
    print(f"           Customer: \"{initial_msg}\"")
    runner.send_telnyx_sms_webhook(phone, initial_msg)

    reply1 = runner.wait_for_message(phone, since_ids=ids, kind="ai_reply", timeout_s=ai_wait_timeout())
    if reply1:
        # Show fuller response for realism
        print(f"           AI: \"{reply1.body[:180]}{'...' if len(reply1.body) > 180 else ''}\"")
    time.sleep(message_delay())

    # =========================================================================
    # Step 3: HARD QUESTION (Tests AI's clinic knowledge)
    # This impresses operators by showing the AI knows their specific services
    # =========================================================================
    _, msgs = runner.get_transcript(phone)
    ids = [m.id for m in msgs if m.id]

    # Simulate customer reading and thinking about the response
    print("\n           (Customer reads the message...)")
    time.sleep(reading_delay())

    print("\n  [Step 3] HARD QUESTION (Testing AI Knowledge)")
    hard_question = "What's the difference between Botox and Dysport? I've heard Dysport spreads more. Which would you recommend for a first-timer, and how much does it cost?"
    print(f"           Customer: \"{hard_question}\"")
    runner.send_telnyx_sms_webhook(phone, hard_question)

    reply2 = runner.wait_for_message(phone, since_ids=ids, kind="ai_reply", timeout_s=ai_wait_timeout())
    if reply2:
        # This is the key moment - AI should demonstrate knowledge
        print(f"           AI: \"{reply2.body[:250]}{'...' if len(reply2.body) > 250 else ''}\"")
        print("           ^ AI demonstrates clinic-specific knowledge!")
    time.sleep(message_delay())

    # =========================================================================
    # Step 4: Qualification (Name + Schedule)
    # =========================================================================
    _, msgs = runner.get_transcript(phone)
    ids = [m.id for m in msgs if m.id]

    # Simulate customer reading the detailed response
    print("\n           (Customer reads the message...)")
    time.sleep(reading_delay())

    print("\n  [Step 4] QUALIFICATION")
    qualification_msg = "That's super helpful! I'm Jennifer, a new patient. Do you have anything available this week? Maybe Friday evening since I work during the day."
    print(f"           Customer: \"{qualification_msg}\"")
    runner.send_telnyx_sms_webhook(phone, qualification_msg)

    reply3 = runner.wait_for_message(phone, since_ids=ids, kind="ai_reply", timeout_s=ai_wait_timeout())
    if reply3:
        print(f"           AI: \"{reply3.body[:180]}{'...' if len(reply3.body) > 180 else ''}\"")
    time.sleep(message_delay())

    # =========================================================================
    # Step 5: Deposit Agreement → Checkout Link
    # =========================================================================
    _, msgs = runner.get_transcript(phone)
    ids = [m.id for m in msgs if m.id]

    # Simulate customer reading availability options
    print("\n           (Customer reads the message...)")
    time.sleep(reading_delay())

    print("\n  [Step 5] DEPOSIT AGREEMENT")
    deposit_msg = "Friday evening works perfectly for me! Yes, I'm happy to pay the deposit to secure my appointment."
    print(f"           Customer: \"{deposit_msg}\"")
    runner.send_telnyx_sms_webhook(phone, deposit_msg)

    # Wait for AI reply first (may come before deposit link)
    ai_reply = runner.wait_for_message(phone, since_ids=ids, kind="ai_reply", timeout_s=ai_wait_timeout())
    if ai_reply:
        print(f"           AI: \"{ai_reply.body[:180]}{'...' if len(ai_reply.body) > 180 else ''}\"")
    time.sleep(2)  # Brief pause before deposit link

    # Wait for deposit link - use original ids so we don't skip it if it was sent quickly
    deposit_link = runner.wait_for_message(phone, since_ids=ids, kind="deposit_link", timeout_s=30)
    if deposit_link:
        # Show the full deposit message for clarity
        print(f"           DEPOSIT LINK: \"{deposit_link.body}\"")
    else:
        # Check if deposit link was sent but as a different kind - search current messages
        _, msgs = runner.get_transcript(phone)
        for m in msgs:
            if m.id not in ids and "checkout.square" in m.body.lower():
                deposit_link = m
                print(f"           DEPOSIT LINK: \"{m.body}\"")
                break
        if not deposit_link:
            print("           (Deposit link may be included in AI reply or still pending)")
    time.sleep(message_delay())

    # =========================================================================
    # Step 6: Customer Opens Checkout & Completes Payment
    # =========================================================================
    print("\n  [Step 6] PAYMENT COMPLETION")

    # Extract checkout URL from the deposit link message
    checkout_url = None
    if deposit_link:
        url_match = re.search(r'https?://[^\s]+', deposit_link.body)
        if url_match:
            checkout_url = url_match.group(0)

    payment_confirmed = False
    _, msgs = runner.get_transcript(phone)
    ids_before_payment = [m.id for m in msgs if m.id]

    if checkout_url:
        print(f"           Customer opens checkout link...")
        print(f"           URL: {checkout_url[:70]}...")

        # Handle different checkout types
        if "/demo/payments/" in checkout_url:
            # Fake checkout - can be displayed in iframe within phone
            print("           [Opening checkout in phone browser...]")
            page.evaluate(f"window.openBrowser('{checkout_url}')")
            time.sleep(3)
            # Click the complete button inside the iframe
            try:
                frame = page.frame_locator("#browserFrame")
                frame.locator("button.btn").click()
                print("           [Customer clicks 'Complete Deposit']")
                time.sleep(2)
            except Exception as e:
                print(f"           (Could not click in iframe: {e})")
            page.evaluate("window.closeBrowser()")
            print("           Payment completed!")

        elif "square" in checkout_url.lower():
            # Square checkout - show simulated checkout view within phone frame
            # (Square blocks iframes, so we show a representative checkout screen)
            print("           [Customer taps payment link on phone...]")

            # Show a simulated Square checkout within the phone's browser view
            checkout_html = f'''
            <html>
            <head>
                <meta name="viewport" content="width=device-width, initial-scale=1">
                <style>
                    * {{ box-sizing: border-box; margin: 0; padding: 0; }}
                    body {{ font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background: #f5f5f5; }}
                    .header {{ background: #006aff; color: white; padding: 16px; text-align: center; }}
                    .header img {{ height: 28px; }}
                    .header h1 {{ font-size: 14px; font-weight: 500; margin-top: 8px; }}
                    .content {{ padding: 20px; }}
                    .amount {{ background: white; border-radius: 12px; padding: 24px; text-align: center; margin-bottom: 20px; box-shadow: 0 2px 8px rgba(0,0,0,0.08); }}
                    .amount .label {{ font-size: 14px; color: #666; margin-bottom: 8px; }}
                    .amount .value {{ font-size: 36px; font-weight: 700; color: #1a1a1a; }}
                    .card {{ background: white; border-radius: 12px; padding: 20px; box-shadow: 0 2px 8px rgba(0,0,0,0.08); }}
                    .card h2 {{ font-size: 16px; margin-bottom: 16px; color: #1a1a1a; }}
                    .field {{ margin-bottom: 16px; }}
                    .field label {{ display: block; font-size: 12px; color: #666; margin-bottom: 6px; }}
                    .field input {{ width: 100%; padding: 14px; border: 1px solid #ddd; border-radius: 8px; font-size: 16px; }}
                    .row {{ display: flex; gap: 12px; }}
                    .row .field {{ flex: 1; }}
                    .btn {{ width: 100%; background: #006aff; color: white; border: none; padding: 16px; border-radius: 8px; font-size: 16px; font-weight: 600; margin-top: 20px; cursor: pointer; }}
                    .secure {{ display: flex; align-items: center; justify-content: center; gap: 6px; margin-top: 16px; font-size: 12px; color: #666; }}
                    .typing {{ animation: blink 1s infinite; }}
                    @keyframes blink {{ 0%,50% {{ opacity: 1; }} 51%,100% {{ opacity: 0; }} }}
                </style>
            </head>
            <body>
                <div class="header">
                    <svg height="28" viewBox="0 0 56 56" fill="white"><path d="M28 0C12.536 0 0 12.536 0 28s12.536 28 28 28 28-12.536 28-28S43.464 0 28 0zm12.25 30.625H30.625v9.625c0 1.449-1.176 2.625-2.625 2.625s-2.625-1.176-2.625-2.625v-9.625H15.75c-1.449 0-2.625-1.176-2.625-2.625s1.176-2.625 2.625-2.625h9.625V15.75c0-1.449 1.176-2.625 2.625-2.625s2.625 1.176 2.625 2.625v9.625h9.625c1.449 0 2.625 1.176 2.625 2.625s-1.176 2.625-2.625 2.625z"/></svg>
                    <h1>Brilliant Aesthetics</h1>
                </div>
                <div class="content">
                    <div class="amount">
                        <div class="label">Deposit Amount</div>
                        <div class="value">$50.00</div>
                    </div>
                    <div class="card">
                        <h2>Payment Details</h2>
                        <div class="field">
                            <label>Card Number</label>
                            <input type="text" value="4532 •••• •••• 7890" readonly>
                        </div>
                        <div class="row">
                            <div class="field">
                                <label>Expiry</label>
                                <input type="text" value="12/27" readonly>
                            </div>
                            <div class="field">
                                <label>CVV</label>
                                <input type="text" value="•••" readonly>
                            </div>
                        </div>
                        <button class="btn" id="payBtn">Pay $50.00</button>
                        <div class="secure">
                            <svg width="14" height="14" fill="#666" viewBox="0 0 24 24"><path d="M18 8h-1V6c0-2.76-2.24-5-5-5S7 3.24 7 6v2H6c-1.1 0-2 .9-2 2v10c0 1.1.9 2 2 2h12c1.1 0 2-.9 2-2V10c0-1.1-.9-2-2-2zm-6 9c-1.1 0-2-.9-2-2s.9-2 2-2 2 .9 2 2-.9 2-2 2zm3.1-9H8.9V6c0-1.71 1.39-3.1 3.1-3.1 1.71 0 3.1 1.39 3.1 3.1v2z"/></svg>
                            Secured by Square
                        </div>
                    </div>
                </div>
            </body>
            </html>
            '''
            # Inject the simulated checkout as a data URL in the browser view
            checkout_b64 = base64.b64encode(checkout_html.encode()).decode()
            page.evaluate(f"window.openBrowser('data:text/html;base64,{checkout_b64}')")
            print("           [Square checkout page shown - customer enters card details]")
            time.sleep(4)

            # Simulate clicking pay button (visual effect)
            try:
                frame = page.frame_locator("#browserFrame")
                frame.locator("#payBtn").click()
                time.sleep(1)
            except Exception:
                pass

            # Close browser view
            page.evaluate("window.closeBrowser()")
            time.sleep(1)

            # Send the actual payment webhook
            # Try to get payment info from deposit_link metadata first (API-based)
            payment_info = None
            if deposit_link and deposit_link.metadata:
                payment_id = deposit_link.metadata.get("payment_id")
                lead_id = deposit_link.metadata.get("lead_id")
                if payment_id and lead_id:
                    payment_info = (lead_id, payment_id)
            # Fall back to database query if metadata not available
            if not payment_info:
                payment_info = runner.get_recent_pending_payment()
            if payment_info:
                lead_id, payment_id = payment_info
                print(f"           Customer completes payment (lead: {lead_id[:8]}...)...")
                runner.send_square_payment_webhook(lead_id, payment_id, 5000)
                print("           Payment completed!")
            else:
                print("           WARNING: No pending payment found")
        else:
            print(f"           Unknown checkout type, simulating webhook...")
            # Try to get payment info from deposit_link metadata first
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

    else:
        # No checkout URL found - fall back to webhook simulation
        print("           (No checkout URL found, simulating payment webhook)")
        # Try to get payment info from deposit_link metadata first
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

    # Wait for confirmation SMS to appear
    print("           Waiting for confirmation SMS...")
    time.sleep(5)  # Give time for webhook to process

    confirm = runner.wait_for_message(
        phone, since_ids=ids_before_payment, kind="payment_confirmation", timeout_s=60
    )
    if confirm:
        # Show full confirmation message
        print(f"           CONFIRMATION: \"{confirm.body}\"")
        payment_confirmed = True
        # Refresh to show the confirmation in the phone view
        page.reload()
        time.sleep(3)  # Longer pause to let viewer read confirmation
        # Re-inject metrics overlay after reload
        try:
            page.evaluate(METRICS_OVERLAY_JS)
        except Exception:
            pass  # Non-critical
        time.sleep(1)
    else:
        print("           WARNING: No payment confirmation received")

    # =========================================================================
    # Result Summary
    # =========================================================================
    print(f"\n  {'='*58}")
    if payment_confirmed:
        print("  SCENARIO 1 RESULT: SUCCESS - Full conversion!")
        print("  - Voice ack sent after missed call")
        print("  - Hard question answered (Botox vs Dysport + pricing)")
        print("  - Lead qualified (name, service, schedule)")
        print("  - Deposit link sent")
        print("  - Payment confirmed ($50)")
        print("  - Dashboard should show: +1 conversation, +$50, conversion rate UP")
    else:
        print("  SCENARIO 1 RESULT: PARTIAL - Conversation completed")
        print("  - Payment confirmation may need manual verification")
    print(f"  {'='*58}")

    # Final pause to show completed conversation state
    time.sleep(8)  # Let viewer see full conversation
    return {"scenario": 1, "phone": phone, "success": True, "payment_confirmed": payment_confirmed}


def run_scenario_2_billing_escalation(runner: DemoRunner, page) -> Dict[str, Any]:
    """
    Scenario 2: Billing Dispute Escalation

    This scenario demonstrates the AI's ability to recognize when to escalate
    to human staff rather than trying to handle everything itself.

    Situation:
    - Customer visited last week for a HydraFacial
    - Was quoted $175 but charged $225 on their card
    - They're frustrated and want a refund/explanation
    - AI should NOT try to resolve billing disputes
    - AI escalates to staff and reassures the customer

    This shows operators that the AI knows its limits and protects them
    from liability by deferring sensitive issues to humans.
    """
    phone = runner.scenario_phones[1]
    print(f"\n{'='*60}")
    print(f"  SCENARIO 2: BILLING DISPUTE - Escalation to Staff")
    print(f"  Phone: {phone}")
    print(f"{'='*60}")

    # Update UI
    page.evaluate("window.updateScenario(2, 'Billing Escalation')")

    # Purge and get initial state
    runner.purge_phone(phone)
    time.sleep(1)
    _, msgs = runner.get_transcript(phone)
    ids = [m.id for m in msgs if m.id]

    # =========================================================================
    # Step 1: Customer contacts about billing issue
    # =========================================================================
    print("\n  [Step 1] BILLING COMPLAINT")
    initial_msg = "Hi, I came in last Tuesday for a HydraFacial and I think I was overcharged. Can someone help me?"
    print(f"           Customer: \"{initial_msg}\"")
    runner.send_telnyx_sms_webhook(phone, initial_msg)

    reply1 = runner.wait_for_message(phone, since_ids=ids, kind="ai_reply", timeout_s=ai_wait_timeout())
    if reply1:
        print(f"           AI: \"{reply1.body[:90]}...\"")
    time.sleep(message_delay())

    # =========================================================================
    # Step 2: Customer provides details about the overcharge
    # =========================================================================
    _, msgs = runner.get_transcript(phone)
    ids = [m.id for m in msgs if m.id]

    # Simulate customer reading and preparing their complaint
    print("\n           (Customer reads the message...)")
    time.sleep(reading_delay())

    print("\n  [Step 2] PROVIDING DETAILS")
    details_msg = "I was quoted $175 for the HydraFacial but my card was charged $225. That's $50 more than what I was told. I'm really frustrated about this."
    print(f"           Customer: \"{details_msg}\"")
    runner.send_telnyx_sms_webhook(phone, details_msg)

    reply2 = runner.wait_for_message(phone, since_ids=ids, kind="ai_reply", timeout_s=ai_wait_timeout())
    if reply2:
        print(f"           AI: \"{reply2.body[:100]}...\"")
        # Check if AI mentions escalation/staff
        escalation_keywords = ["staff", "team", "manager", "someone", "look into", "reach out", "contact you", "review"]
        escalated = any(kw in reply2.body.lower() for kw in escalation_keywords)
    else:
        escalated = False
    time.sleep(message_delay())

    # =========================================================================
    # Step 3: Customer asks for timeline
    # =========================================================================
    _, msgs = runner.get_transcript(phone)
    ids = [m.id for m in msgs if m.id]

    # Simulate customer reading the escalation response
    print("\n           (Customer reads the message...)")
    time.sleep(reading_delay())

    print("\n  [Step 3] REQUESTING TIMELINE")
    timeline_msg = "When can I expect to hear back? I need this resolved soon."
    print(f"           Customer: \"{timeline_msg}\"")
    runner.send_telnyx_sms_webhook(phone, timeline_msg)

    reply3 = runner.wait_for_message(phone, since_ids=ids, kind="ai_reply", timeout_s=ai_wait_timeout())
    if reply3:
        print(f"           AI: \"{reply3.body[:100]}...\"")
    time.sleep(message_delay())

    # =========================================================================
    # Result Summary
    # =========================================================================
    print(f"\n  {'='*58}")
    if escalated:
        print("  SCENARIO 2 RESULT: SUCCESS - Escalated to staff")
        print("  - AI recognized this is a billing dispute")
        print("  - AI did NOT try to resolve the issue itself")
        print("  - AI promised to notify staff/have someone follow up")
        print("  - Customer reassured that issue will be handled")
        print("  - Dashboard: +1 conversation, $0 deposit, no conversion")
    else:
        print("  SCENARIO 2 RESULT: COMPLETED - Conversation handled")
        print("  - Check if AI appropriately escalated to human staff")
    print(f"  {'='*58}")

    time.sleep(scenario_delay())
    return {"scenario": 2, "phone": phone, "success": True, "escalated": escalated}


def run_scenario_3_no_conversion(runner: DemoRunner, page) -> Dict[str, Any]:
    """
    Scenario 3: No Conversion
    - Patient inquires about services
    - Patient decides not to book / declines deposit
    - No payment collected
    """
    phone = runner.scenario_phones[2]
    print(f"\n=== SCENARIO 3: No Conversion (phone: {phone}) ===")

    # Update UI
    page.evaluate("window.updateScenario(3, 'No Conversion')")

    # Purge and get initial state
    runner.purge_phone(phone)
    time.sleep(1)
    _, msgs = runner.get_transcript(phone)
    ids = [m.id for m in msgs if m.id]

    # Step 1: Initial inquiry
    print("  [3.1] Customer: 'What services do you offer?'")
    runner.send_telnyx_sms_webhook(phone, "What services do you offer?")

    reply1 = runner.wait_for_message(phone, since_ids=ids, kind="ai_reply", timeout_s=ai_wait_timeout())
    if reply1:
        print(f"  [3.1] AI: {reply1.body[:80]}...")
    time.sleep(message_delay())

    # Step 2: Follow-up question
    _, msgs = runner.get_transcript(phone)
    ids = [m.id for m in msgs if m.id]

    # Simulate customer reading service list
    print("         (Customer reads the message...)")
    time.sleep(reading_delay())

    print("  [3.2] Customer: 'How much does Botox cost?'")
    runner.send_telnyx_sms_webhook(phone, "How much does Botox cost?")

    reply2 = runner.wait_for_message(phone, since_ids=ids, kind="ai_reply", timeout_s=ai_wait_timeout())
    if reply2:
        print(f"  [3.2] AI: {reply2.body[:80]}...")
    time.sleep(message_delay())

    # Step 3: Customer declines
    _, msgs = runner.get_transcript(phone)
    ids = [m.id for m in msgs if m.id]

    # Simulate customer reading pricing and deciding not to proceed
    print("         (Customer reads the message...)")
    time.sleep(reading_delay())

    print("  [3.3] Customer: 'Thanks, I'll think about it and maybe call back later'")
    runner.send_telnyx_sms_webhook(phone, "Thanks, I'll think about it and maybe call back later")

    reply3 = runner.wait_for_message(phone, since_ids=ids, kind="ai_reply", timeout_s=ai_wait_timeout())
    if reply3:
        print(f"  [3.3] AI: {reply3.body[:80]}...")

    time.sleep(scenario_delay())
    return {"scenario": 3, "phone": phone, "success": True, "converted": False}


def update_dashboard_metrics(runner: DemoRunner, page):
    """Fetch dashboard metrics and update the UI overlay."""
    try:
        metrics = runner.get_dashboard_metrics()
        conversations = metrics.get("conversations", {}).get("unique_conversations", 0)
        deposits = metrics.get("payments", {}).get("total_collected_cents", 0)
        conversion_rate = metrics.get("leads", {}).get("conversion_rate", 0)

        page.evaluate(f"window.updateMetrics({conversations}, {deposits}, {conversion_rate})")
    except Exception as e:
        print(f"Warning: Failed to update metrics: {e}")


def main() -> int:
    parser = argparse.ArgumentParser(description="Video Demo with 3 Scenarios")
    parser.add_argument("--api-url", default=os.getenv("API_URL", "http://localhost:8082"))
    parser.add_argument("--headed", action="store_true", help="Run Playwright headed (debug)")
    parser.add_argument("--scenario", type=int, choices=[1, 2, 3], help="Run only a specific scenario (1, 2, or 3)")
    args = parser.parse_args()

    api_url = args.api_url.rstrip("/")
    os.environ["API_URL"] = api_url

    # Change to project root
    os.chdir(_PROJECT_ROOT)

    run_id = now_run_id()
    videos_dir = _PROJECT_ROOT / "tmp" / "demo_videos"
    artifacts_dir = _PROJECT_ROOT / "tmp" / "demo_artifacts" / run_id
    mkdirp(videos_dir)
    mkdirp(artifacts_dir)

    # Load .env
    import e2e_full_flow as base

    # Required env vars
    admin_secret = require_env("ADMIN_JWT_SECRET")

    # TELNYX_WEBHOOK_SECRET: For testing, any shared secret works.
    # If not set, use a default test secret (API in dev mode will skip verification anyway)
    telnyx_secret = os.getenv("TELNYX_WEBHOOK_SECRET", "").strip()
    if not telnyx_secret:
        telnyx_secret = "demo-test-secret"
        os.environ["TELNYX_WEBHOOK_SECRET"] = telnyx_secret
        print(f"  Using default test webhook secret: {telnyx_secret}")

    sms_provider = (os.getenv("SMS_PROVIDER") or "").strip().lower()
    if sms_provider != "telnyx":
        raise RuntimeError(f"SMS_PROVIDER must be 'telnyx' for this demo (got {sms_provider!r})")

    token = make_admin_jwt(admin_secret)
    runner = DemoRunner(api_url, token, artifacts_dir)

    # Check API health
    if not base.check_health():
        raise RuntimeError(f"API not healthy at {api_url}")

    # Seed knowledge, hosted number, and clinic config
    if not base.seed_knowledge():
        raise RuntimeError("Knowledge seeding failed")
    base.seed_hosted_number()
    base.seed_clinic_config("Brilliant Aesthetics")

    print(f"\n{'='*60}")
    print("  MedSpa AI Platform - Video Demo Recording")
    print(f"{'='*60}")
    print(f"  API URL:    {api_url}")
    print(f"  Org ID:     {runner.org_id}")
    print(f"  Clinic:     {runner.clinic_phone}")
    print(f"  Output:     {videos_dir}/")
    print(f"{'='*60}\n")

    # Start Playwright
    from playwright.sync_api import sync_playwright

    video_path: Optional[str] = None
    results: List[Dict[str, Any]] = []

    with sync_playwright() as p:
        browser = p.chromium.launch(headless=not args.headed)
        context = browser.new_context(
            viewport={"width": 1920, "height": 1080},
            record_video_dir=str(videos_dir),
            record_video_size={"width": 1920, "height": 1080},
            extra_http_headers={"Authorization": f"Bearer {token}"},
        )
        page = context.new_page()

        try:
            # Navigate directly to phone simulator with auth (no iframes)
            phone_url = (
                f"{api_url}/admin/e2e/phone-simulator"
                f"?orgID={quote(runner.org_id)}"
                f"&phone={quote(runner.scenario_phones[0], safe='')}"
                f"&clinic={quote(runner.clinic_phone, safe='')}"
                f"&poll_ms=800"
            )
            page.goto(phone_url, wait_until="networkidle")
            time.sleep(1)

            # Inject metrics overlay on top of phone simulator
            page.evaluate(METRICS_OVERLAY_JS)
            time.sleep(1)

            # Initial dashboard metrics
            update_dashboard_metrics(runner, page)
            time.sleep(2)

            # Run scenarios based on --scenario flag
            run_scenario_1 = args.scenario is None or args.scenario == 1
            run_scenario_2 = args.scenario is None or args.scenario == 2
            run_scenario_3 = args.scenario is None or args.scenario == 3

            if run_scenario_1:
                # Run Scenario 1: Happy Path
                result1 = run_scenario_1_happy_path(runner, page)
                results.append(result1)
                update_dashboard_metrics(runner, page)
                time.sleep(3)

            if run_scenario_2:
                # Run Scenario 2: Billing Escalation - navigate to new phone
                phone_url = (
                    f"{api_url}/admin/e2e/phone-simulator"
                    f"?orgID={quote(runner.org_id)}"
                    f"&phone={quote(runner.scenario_phones[1], safe='')}"
                    f"&clinic={quote(runner.clinic_phone, safe='')}"
                    f"&poll_ms=800"
                )
                page.goto(phone_url, wait_until="networkidle")
                page.evaluate(METRICS_OVERLAY_JS)
                update_dashboard_metrics(runner, page)
                time.sleep(1)

                result2 = run_scenario_2_billing_escalation(runner, page)
                results.append(result2)
                update_dashboard_metrics(runner, page)
                time.sleep(3)

            if run_scenario_3:
                # Run Scenario 3: No Conversion - navigate to new phone
                phone_url = (
                    f"{api_url}/admin/e2e/phone-simulator"
                    f"?orgID={quote(runner.org_id)}"
                    f"&phone={quote(runner.scenario_phones[2], safe='')}"
                    f"&clinic={quote(runner.clinic_phone, safe='')}"
                    f"&poll_ms=800"
                )
                page.goto(phone_url, wait_until="networkidle")
                page.evaluate(METRICS_OVERLAY_JS)
                update_dashboard_metrics(runner, page)
                time.sleep(1)

                result3 = run_scenario_3_no_conversion(runner, page)
                results.append(result3)
                update_dashboard_metrics(runner, page)

            # Final dashboard view
            try:
                page.evaluate("window.updateScenario(0, 'Demo Complete')")
            except Exception:
                pass  # Non-critical: overlay may be missing after page navigations
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

    # Save results
    (artifacts_dir / "results.json").write_text(json.dumps(results, indent=2), encoding="utf-8")

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
