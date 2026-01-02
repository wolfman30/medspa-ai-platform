#!/usr/bin/env python3
"""
E2E (Telnyx + Square) with Phone-View Video Recording

Runs:
  - Preflight: purge phone + compliance checks (STOP/HELP/START/YES, PCI, idempotency, first-contact ack).
  - Happy path: missed call -> SMS -> deposit link -> Square webhook -> confirmation -> priority booking DB checks.
  - Records a mobile-viewport video of the phone simulator UI.

Artifacts:
  - Video: tmp/e2e_videos/
  - Debug: tmp/e2e_artifacts/<run_id>/
"""

from __future__ import annotations

import argparse
import base64
import hashlib
import hmac
import json
import os
import sys
import time
import traceback
import uuid
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Dict, List, Optional, Sequence, Tuple
from urllib.parse import quote


def _b64url(data: bytes) -> str:
    return base64.urlsafe_b64encode(data).rstrip(b"=").decode("ascii")


def make_admin_jwt(secret: str, *, ttl_seconds: int = 20 * 60) -> str:
    now = int(time.time())
    header = {"alg": "HS256", "typ": "JWT"}
    payload = {"iat": now, "exp": now + int(ttl_seconds)}

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


def admin_headers(token: str) -> Dict[str, str]:
    return {"Authorization": f"Bearer {token}"}


def http_timeout() -> float:
    return float(os.getenv("E2E_HTTP_TIMEOUT", "20"))


def max_ai_seconds() -> float:
    return float(os.getenv("E2E_MAX_AI_STEP_SECONDS", "45"))


def fail_on_latency() -> bool:
    return (os.getenv("E2E_FAIL_ON_AI_LATENCY", "") or "").strip().lower() in ("1", "true", "yes", "on")


def step_delay_seconds() -> float:
    raw = (os.getenv("E2E_STEP_DELAY_SECONDS") or "").strip()
    if raw:
        try:
            return float(raw)
        except ValueError:
            raise RuntimeError(f"Invalid E2E_STEP_DELAY_SECONDS: {raw!r}")
    raw_ms = (os.getenv("E2E_STEP_DELAY_MS") or "").strip()
    if raw_ms:
        try:
            return float(raw_ms) / 1000.0
        except ValueError:
            raise RuntimeError(f"Invalid E2E_STEP_DELAY_MS: {raw_ms!r}")
    return 0.0


def pace(label: str) -> None:
    delay = step_delay_seconds()
    if delay <= 0:
        return
    print(f"ℹ️  pacing {label}: sleeping {delay:.1f}s", file=sys.stderr)
    time.sleep(delay)


@dataclass(frozen=True)
class TranscriptMessage:
    id: str
    role: str
    body: str
    kind: str
    timestamp: str


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
            )
        )
    return out


def get_sms_transcript(api_url: str, org_id: str, phone: str, token: str) -> Tuple[str, List[TranscriptMessage]]:
    try:
        import requests
    except ImportError as e:
        raise RuntimeError("Missing dependency: requests (pip install requests)") from e

    url = f"{api_url}/admin/clinics/{quote(org_id)}/sms/{quote(phone, safe='')}" + "?limit=500"
    resp = requests.get(url, headers=admin_headers(token), timeout=http_timeout())
    if resp.status_code != 200:
        raise RuntimeError(f"GET {url} failed: {resp.status_code} {resp.text[:300]}")
    data = resp.json() or {}
    conversation_id = str(data.get("conversation_id") or "")
    return conversation_id, _parse_transcript_messages(data)


def purge_phone(api_url: str, org_id: str, phone: str, token: str) -> None:
    try:
        import requests
    except ImportError as e:
        raise RuntimeError("Missing dependency: requests (pip install requests)") from e

    url = f"{api_url}/admin/clinics/{quote(org_id)}/phones/{quote(phone, safe='')}"
    resp = requests.delete(url, headers=admin_headers(token), timeout=http_timeout())
    if resp.status_code not in (200, 204):
        raise RuntimeError(f"DELETE {url} failed: {resp.status_code} {resp.text[:300]}")


def wait_for_new_message(
    api_url: str,
    org_id: str,
    phone: str,
    token: str,
    *,
    since_ids: Sequence[str],
    kind: Optional[str] = None,
    kinds: Optional[Sequence[str]] = None,
    role: Optional[str] = None,
    roles: Optional[Sequence[str]] = None,
    contains: Optional[str] = None,
    timeout_s: float = 30.0,
    poll_s: float = 0.6,
) -> TranscriptMessage:
    deadline = time.time() + timeout_s
    since = set(since_ids)
    want_kind = (kind or "").strip()
    want_kinds = [str(k).strip() for k in (kinds or []) if str(k).strip()]
    want_role = (role or "").strip()
    want_roles = [str(r).strip() for r in (roles or []) if str(r).strip()]
    want_contains = (contains or "")

    last_msgs: List[TranscriptMessage] = []
    while time.time() < deadline:
        _, msgs = get_sms_transcript(api_url, org_id, phone, token)
        last_msgs = msgs
        for m in msgs:
            if m.id and m.id in since:
                continue
            if want_kinds and m.kind not in want_kinds:
                continue
            if want_kind and not want_kinds and m.kind != want_kind:
                continue
            if want_roles and m.role not in want_roles:
                continue
            if want_role and not want_roles and m.role != want_role:
                continue
            if want_contains and want_contains not in m.body:
                continue
            return m
        time.sleep(poll_s)
    sample = "\n".join([f"- {m.kind}/{m.role}: {m.body[:120]}" for m in last_msgs[-10:]])
    want_kind_desc = want_kinds if want_kinds else want_kind
    want_role_desc = want_roles if want_roles else want_role
    raise RuntimeError(
        f"Timeout waiting for message kind={want_kind_desc!r} role={want_role_desc!r} contains={want_contains!r}\nRecent:\n{sample}"
    )


def assert_no_new_messages(
    api_url: str,
    org_id: str,
    phone: str,
    token: str,
    *,
    since_ids: Sequence[str],
    kind: Optional[str] = None,
    kinds: Optional[Sequence[str]] = None,
    role: Optional[str] = None,
    roles: Optional[Sequence[str]] = None,
    contains: Optional[str] = None,
    timeout_s: float = 6.0,
    poll_s: float = 0.6,
) -> None:
    deadline = time.time() + timeout_s
    since = set(since_ids)
    want_kind = (kind or "").strip()
    want_kinds = [str(k).strip() for k in (kinds or []) if str(k).strip()]
    want_role = (role or "").strip()
    want_roles = [str(r).strip() for r in (roles or []) if str(r).strip()]
    want_contains = (contains or "")

    while time.time() < deadline:
        _, msgs = get_sms_transcript(api_url, org_id, phone, token)
        for m in msgs:
            if m.id and m.id in since:
                continue
            if want_kinds and m.kind not in want_kinds:
                continue
            if want_kind and not want_kinds and m.kind != want_kind:
                continue
            if want_roles and m.role not in want_roles:
                continue
            if want_role and not want_roles and m.role != want_role:
                continue
            if want_contains and want_contains not in m.body:
                continue
            raise RuntimeError(f"Expected no matching messages, but saw: kind={m.kind} role={m.role} body={m.body[:200]!r}")
        time.sleep(poll_s)


def assert_no_new_assistant_messages(
    api_url: str,
    org_id: str,
    phone: str,
    token: str,
    *,
    since_ids: Sequence[str],
    timeout_s: float = 6.0,
    poll_s: float = 0.6,
) -> None:
    deadline = time.time() + timeout_s
    since = set(since_ids)
    while time.time() < deadline:
        _, msgs = get_sms_transcript(api_url, org_id, phone, token)
        for m in msgs:
            if m.id and m.id in since:
                continue
            if m.role == "assistant":
                raise RuntimeError(f"Expected no new assistant messages, but saw: kind={m.kind} body={m.body[:160]!r}")
        time.sleep(poll_s)


def _last_inbound_message(messages: Sequence[TranscriptMessage]) -> Optional[TranscriptMessage]:
    for m in reversed(messages):
        if m.role == "user" and m.kind == "inbound":
            return m
    return None


def assert_audit_event(
    org_id: str,
    event_type: str,
    *,
    conversation_id: Optional[str] = None,
    since_minutes: int = 10,
) -> None:
    import e2e_full_flow as base

    sql = (
        "SELECT COUNT(*) FROM compliance_audit_events "
        f"WHERE org_id = '{org_id}' AND event_type = '{event_type}' "
        f"AND created_at >= NOW() - interval '{since_minutes} minutes'"
    )
    if conversation_id:
        sql += f" AND conversation_id = '{conversation_id}'"
    proc = base.run_psql(sql, tuples_only=True, timeout=10)
    if proc is None:
        raise RuntimeError("psql not available (and docker compose fallback not found); cannot verify audit events")
    if proc.returncode != 0:
        raise RuntimeError(f"Audit query failed: {proc.stderr[:300]}")
    raw = (proc.stdout or "").strip()
    try:
        count = int(raw or "0")
    except ValueError as e:
        raise RuntimeError(f"Unexpected audit query result: {raw!r}") from e
    if count < 1:
        raise RuntimeError(f"Expected audit event {event_type!r} not found (conversation_id={conversation_id!r})")


def run_compliance_suite(api_url: str, token: str) -> None:
    import e2e_full_flow as base

    org_id = base.TEST_ORG_ID
    customer = base.TEST_CUSTOMER_PHONE

    if getattr(base, "SKIP_DB_CHECK", False):
        raise RuntimeError("scripts/e2e_with_video.py requires DB checks; unset SKIP_DB_CHECK or set SKIP_DB_CHECK=0")

    if not base.seed_hosted_number():
        raise RuntimeError("Hosted number seeding failed; Telnyx webhooks will 404 without a clinic mapping")

    run_tag = uuid.uuid4().hex[:8]
    demo_mode = (os.getenv("DEMO_MODE") or "").strip().lower() in ("1", "true", "yes", "on")
    first_contact_reply = (os.getenv("TELNYX_FIRST_CONTACT_REPLY") or "").strip()

    purge_phone(api_url, org_id, customer, token)
    _, msgs = get_sms_transcript(api_url, org_id, customer, token)
    start_ids = [m.id for m in msgs if m.id]

    # 1) First-contact ack only once (when configured)
    pace("first_contact_1")
    base.send_telnyx_sms_webhook(
        "Hi - quick question",
        telnyx_message_id=f"msg_ci_first_{run_tag}",
        event_id=f"evt_ci_first_{run_tag}",
    )
    ack1 = wait_for_new_message(
        api_url,
        org_id,
        customer,
        token,
        since_ids=start_ids,
        kinds=("ack", "first_contact_ack"),
        roles=("assistant",),
        timeout_s=20.0,
    )
    if demo_mode and first_contact_reply and ack1.kind != "first_contact_ack":
        raise RuntimeError(
            "Expected first-contact auto-reply to be sent on first inbound when DEMO_MODE=1 and TELNYX_FIRST_CONTACT_REPLY is set"
        )

    _, msgs = get_sms_transcript(api_url, org_id, customer, token)
    ids_after_first = [m.id for m in msgs if m.id]

    pace("first_contact_2")
    base.send_telnyx_sms_webhook(
        "Following up",
        telnyx_message_id=f"msg_ci_second_{run_tag}",
        event_id=f"evt_ci_second_{run_tag}",
    )
    ack2 = wait_for_new_message(
        api_url,
        org_id,
        customer,
        token,
        since_ids=ids_after_first,
        kinds=("ack", "first_contact_ack"),
        roles=("assistant",),
        timeout_s=20.0,
    )
    if ack1.kind == "first_contact_ack" and ack2.kind == "first_contact_ack":
        raise RuntimeError("first-contact auto-reply was sent more than once (expected only on first inbound)")

    # 2) HELP
    _, msgs = get_sms_transcript(api_url, org_id, customer, token)
    ids = [m.id for m in msgs if m.id]
    pace("help")
    base.send_telnyx_sms_webhook(
        "HELP",
        telnyx_message_id=f"msg_help_{run_tag}",
        event_id=f"evt_help_{run_tag}",
    )
    help_ack = wait_for_new_message(api_url, org_id, customer, token, since_ids=ids, kind="help_ack", timeout_s=10.0)
    expected_help = os.getenv("TELNYX_HELP_REPLY", "Reply STOP to opt out or contact support@medspa.ai.")
    if help_ack.body.strip() != expected_help.strip():
        raise RuntimeError(f"HELP reply mismatch.\nExpected: {expected_help!r}\nActual:   {help_ack.body!r}")

    # 3) STOP, then opt-out suppression
    _, msgs = get_sms_transcript(api_url, org_id, customer, token)
    ids = [m.id for m in msgs if m.id]
    pace("stop")
    base.send_telnyx_sms_webhook(
        "STOP",
        telnyx_message_id=f"msg_stop_{run_tag}",
        event_id=f"evt_stop_{run_tag}",
    )
    stop_ack = wait_for_new_message(api_url, org_id, customer, token, since_ids=ids, kind="stop_ack", timeout_s=10.0)
    expected_stop = os.getenv("TELNYX_STOP_REPLY", "You have been opted out. Reply HELP for info.")
    if stop_ack.body.strip() != expected_stop.strip():
        raise RuntimeError(f"STOP reply mismatch.\nExpected: {expected_stop!r}\nActual:   {stop_ack.body!r}")

    _, msgs = get_sms_transcript(api_url, org_id, customer, token)
    ids = [m.id for m in msgs if m.id]
    pace("after_stop")
    base.send_telnyx_sms_webhook(
        "Are you still there?",
        telnyx_message_id=f"msg_after_stop_{run_tag}",
        event_id=f"evt_after_stop_{run_tag}",
    )
    assert_no_new_assistant_messages(api_url, org_id, customer, token, since_ids=ids, timeout_s=6.0)

    # 4) START (or demo YES)
    _, msgs = get_sms_transcript(api_url, org_id, customer, token)
    ids = [m.id for m in msgs if m.id]
    pace("start")
    base.send_telnyx_sms_webhook(
        "START",
        telnyx_message_id=f"msg_start_{run_tag}",
        event_id=f"evt_start_{run_tag}",
    )
    start_ack = wait_for_new_message(api_url, org_id, customer, token, since_ids=ids, kind="start_ack", timeout_s=10.0)
    expected_start = os.getenv("TELNYX_START_REPLY", "You're opted back in. Reply STOP to opt out.")
    if start_ack.body.strip() != expected_start.strip():
        raise RuntimeError(f"START reply mismatch.\nExpected: {expected_start!r}\nActual:   {start_ack.body!r}")

    # Demo-mode YES opt-in should behave like START (exact keyword).
    if demo_mode:
        _, msgs = get_sms_transcript(api_url, org_id, customer, token)
        ids = [m.id for m in msgs if m.id]
        pace("demo_stop")
        base.send_telnyx_sms_webhook(
            "STOP",
            telnyx_message_id=f"msg_stop_2_{run_tag}",
            event_id=f"evt_stop_2_{run_tag}",
        )
        _ = wait_for_new_message(api_url, org_id, customer, token, since_ids=ids, kind="stop_ack", timeout_s=10.0)

        _, msgs = get_sms_transcript(api_url, org_id, customer, token)
        ids = [m.id for m in msgs if m.id]
        pace("demo_yes")
        base.send_telnyx_sms_webhook(
            "YES",
            telnyx_message_id=f"msg_yes_{run_tag}",
            event_id=f"evt_yes_{run_tag}",
        )
        yes_ack = wait_for_new_message(api_url, org_id, customer, token, since_ids=ids, kind="start_ack", timeout_s=10.0)
        if yes_ack.body.strip() != expected_start.strip():
            raise RuntimeError(f"YES opt-in reply mismatch.\nExpected: {expected_start!r}\nActual:   {yes_ack.body!r}")
        _, msgs = get_sms_transcript(api_url, org_id, customer, token)
        ids = [m.id for m in msgs if m.id]
        assert_no_new_messages(api_url, org_id, customer, token, since_ids=ids, kind="ai_reply", timeout_s=6.0)

    # 5) PCI guardrail (PAN-like)
    purge_phone(api_url, org_id, customer, token)
    _, msgs = get_sms_transcript(api_url, org_id, customer, token)
    ids = [m.id for m in msgs if m.id]
    pace("pci_guardrail")
    base.send_telnyx_sms_webhook(
        "My card is 4111 1111 1111 1111",
        telnyx_message_id=f"msg_pan_{run_tag}",
        event_id=f"evt_pan_{run_tag}",
    )
    guard = wait_for_new_message(api_url, org_id, customer, token, since_ids=ids, kind="pci_guardrail", timeout_s=10.0)
    expected_guard = 'For your security, please do not send credit card details by text.'
    if expected_guard not in guard.body:
        raise RuntimeError(f"PCI guardrail reply missing expected prefix. Got: {guard.body!r}")
    _, msgs = get_sms_transcript(api_url, org_id, customer, token)
    ids = [m.id for m in msgs if m.id]
    assert_no_new_messages(api_url, org_id, customer, token, since_ids=ids, kind="ai_reply", timeout_s=6.0)

    # 6) Medical advice deflection (non-PHI)
    purge_phone(api_url, org_id, customer, token)
    conversation_id, msgs = get_sms_transcript(api_url, org_id, customer, token)
    ids = [m.id for m in msgs if m.id]
    pace("medical_advice")
    base.send_telnyx_sms_webhook(
        "Is it safe for me to take ibuprofen before Botox?",
        telnyx_message_id=f"msg_medical_{run_tag}",
        event_id=f"evt_medical_{run_tag}",
    )
    _ = wait_for_new_message(
        api_url,
        org_id,
        customer,
        token,
        since_ids=ids,
        kinds=("ack", "first_contact_ack"),
        roles=("assistant",),
        timeout_s=20.0,
    )
    med_reply = wait_for_new_message(api_url, org_id, customer, token, since_ids=ids, kind="ai_reply", timeout_s=120.0)
    if "can't provide medical advice" not in med_reply.body.lower():
        raise RuntimeError(f"Medical advice deflection missing expected text: {med_reply.body!r}")
    conversation_id, msgs = get_sms_transcript(api_url, org_id, customer, token)
    inbound = _last_inbound_message(msgs)
    if inbound is None or inbound.body.strip() != "[REDACTED]":
        got_body = inbound.body if inbound else None
        raise RuntimeError(f"Expected inbound medical advice to be redacted, got: {got_body!r}")
    assert_audit_event(org_id, "compliance.medical_advice_refused", conversation_id=conversation_id)

    # 7) PHI deflection (redaction + audit)
    purge_phone(api_url, org_id, customer, token)
    conversation_id, msgs = get_sms_transcript(api_url, org_id, customer, token)
    ids = [m.id for m in msgs if m.id]
    pace("phi_deflection")
    base.send_telnyx_sms_webhook(
        "I have diabetes and need advice about Botox.",
        telnyx_message_id=f"msg_phi_{run_tag}",
        event_id=f"evt_phi_{run_tag}",
    )
    _ = wait_for_new_message(
        api_url,
        org_id,
        customer,
        token,
        since_ids=ids,
        kinds=("ack", "first_contact_ack"),
        roles=("assistant",),
        timeout_s=20.0,
    )
    phi_reply = wait_for_new_message(api_url, org_id, customer, token, since_ids=ids, kind="ai_reply", timeout_s=120.0)
    if "can't provide medical advice" not in phi_reply.body.lower():
        raise RuntimeError(f"PHI deflection missing expected text: {phi_reply.body!r}")
    conversation_id, msgs = get_sms_transcript(api_url, org_id, customer, token)
    inbound = _last_inbound_message(msgs)
    if inbound is None or inbound.body.strip() != "[REDACTED]":
        got_body = inbound.body if inbound else None
        raise RuntimeError(f"Expected inbound PHI to be redacted, got: {got_body!r}")
    assert_audit_event(org_id, "compliance.phi_detected", conversation_id=conversation_id)

    # 8) Idempotency: duplicate Telnyx message_id does not enqueue twice
    purge_phone(api_url, org_id, customer, token)
    _, msgs = get_sms_transcript(api_url, org_id, customer, token)
    ids = [m.id for m in msgs if m.id]
    dup_id = f"msg_dup_{run_tag}"
    pace("idempotency_1")
    base.send_telnyx_sms_webhook("Hello once", telnyx_message_id=dup_id, event_id=f"evt_dup_1_{run_tag}")
    _ = wait_for_new_message(
        api_url,
        org_id,
        customer,
        token,
        since_ids=ids,
        kinds=("ack", "first_contact_ack"),
        roles=("assistant",),
        timeout_s=20.0,
    )
    _ = wait_for_new_message(api_url, org_id, customer, token, since_ids=ids, kind="ai_reply", timeout_s=120.0)
    _, msgs = get_sms_transcript(api_url, org_id, customer, token)
    ids2 = [m.id for m in msgs if m.id]
    pace("idempotency_2")
    base.send_telnyx_sms_webhook("Hello once", telnyx_message_id=dup_id, event_id=f"evt_dup_2_{run_tag}")
    # Expect no new assistant messages (dedup should short-circuit early)
    assert_no_new_assistant_messages(api_url, org_id, customer, token, since_ids=ids2, timeout_s=6.0)


def run_happy_path(api_url: str, token: str, *, artifacts_dir: Path) -> Dict[str, Any]:
    import e2e_full_flow as base

    org_id = base.TEST_ORG_ID
    customer = base.TEST_CUSTOMER_PHONE
    clinic = base.TEST_CLINIC_PHONE
    run_tag = uuid.uuid4().hex[:8]

    if getattr(base, "SKIP_DB_CHECK", False):
        raise RuntimeError("scripts/e2e_with_video.py requires DB checks; unset SKIP_DB_CHECK or set SKIP_DB_CHECK=0")

    purge_phone(api_url, org_id, customer, token)

    # Ensure hosted number mapping exists for webhook routing.
    base.seed_hosted_number()

    # Create a lead so we can deterministically locate the payment record.
    lead = base.create_lead()
    lead_id = (lead or {}).get("id") if isinstance(lead, dict) else None

    timings: Dict[str, float] = {}
    expected_amount_cents = int(os.getenv("DEPOSIT_AMOUNT_CENTS", "5000"))

    def record_ai_latency(step_name: str, started_at: float, finished_at: float) -> None:
        seconds = max(0.0, finished_at - started_at)
        timings[step_name] = seconds
        if seconds > max_ai_seconds():
            msg = f"AI latency exceeded: {step_name} took {seconds:.1f}s (max {max_ai_seconds():.1f}s)"
            if fail_on_latency():
                raise RuntimeError(msg)
            print(f"WARNING: {msg}", file=sys.stderr)

    # Missed call -> immediate ack
    _, msgs = get_sms_transcript(api_url, org_id, customer, token)
    ids = [m.id for m in msgs if m.id]
    pace("voice_ack")
    if not base.send_telnyx_voice_webhook(event_id=f"evt_voice_1_{run_tag}", call_id=f"call_voice_1_{run_tag}"):
        raise RuntimeError("Voice webhook failed")
    _ = wait_for_new_message(api_url, org_id, customer, token, since_ids=ids, kind="voice_ack", timeout_s=20.0)

    # Customer SMS 1
    _, msgs = get_sms_transcript(api_url, org_id, customer, token)
    ids = [m.id for m in msgs if m.id]
    t0 = time.time()
    pace("sms_1")
    base.send_telnyx_sms_webhook(
        "Hi, I want to book Botox for weekday afternoons",
        telnyx_message_id=f"msg_hp_1_{run_tag}",
        event_id=f"evt_hp_1_{run_tag}",
    )
    _ = wait_for_new_message(
        api_url,
        org_id,
        customer,
        token,
        since_ids=ids,
        kinds=("ack", "first_contact_ack"),
        timeout_s=20.0,
    )
    _ = wait_for_new_message(api_url, org_id, customer, token, since_ids=ids, kind="ai_reply", timeout_s=240.0)
    record_ai_latency("ai_reply_1", t0, time.time())

    # Customer SMS 2
    _, msgs = get_sms_transcript(api_url, org_id, customer, token)
    ids = [m.id for m in msgs if m.id]
    t0 = time.time()
    pace("sms_2")
    base.send_telnyx_sms_webhook(
        "Yes, I'm a new patient. What times do you have available?",
        telnyx_message_id=f"msg_hp_2_{run_tag}",
        event_id=f"evt_hp_2_{run_tag}",
    )
    _ = wait_for_new_message(
        api_url,
        org_id,
        customer,
        token,
        since_ids=ids,
        kinds=("ack", "first_contact_ack"),
        timeout_s=20.0,
    )
    _ = wait_for_new_message(api_url, org_id, customer, token, since_ids=ids, kind="ai_reply", timeout_s=240.0)
    record_ai_latency("ai_reply_2", t0, time.time())

    # Customer SMS 3: deposit intent
    _, msgs = get_sms_transcript(api_url, org_id, customer, token)
    ids = [m.id for m in msgs if m.id]
    t0 = time.time()
    pace("sms_3")
    base.send_telnyx_sms_webhook(
        "Friday at 3pm works great. Yes, I'll pay the deposit to secure my appointment.",
        telnyx_message_id=f"msg_hp_3_{run_tag}",
        event_id=f"evt_hp_3_{run_tag}",
    )
    _ = wait_for_new_message(
        api_url,
        org_id,
        customer,
        token,
        since_ids=ids,
        kinds=("ack", "first_contact_ack"),
        timeout_s=20.0,
    )
    _ = wait_for_new_message(api_url, org_id, customer, token, since_ids=ids, kind="ai_reply", timeout_s=240.0)
    record_ai_latency("ai_reply_3", t0, time.time())

    # Deposit link (from deposit dispatcher)
    deposit_msg = wait_for_new_message(api_url, org_id, customer, token, since_ids=ids, kind="deposit_link", timeout_s=240.0)
    if "http" not in deposit_msg.body:
        raise RuntimeError(f"Deposit link message did not contain a URL: {deposit_msg.body!r}")

    # Square payment webhook (simulated)
    if not lead_id:
        raise RuntimeError("Lead ID missing (create_lead failed); cannot locate payment record for Square webhook")
    booking_intent_id = base.get_payment_id_for_lead(lead_id)
    if not booking_intent_id:
        raise RuntimeError("Could not find payment ID (booking_intent_id) for lead; ensure DB is reachable and deposit intent created a payment row")

    try:
        proc = base.run_psql(
            f"SELECT amount_cents FROM payments WHERE id = '{booking_intent_id}' LIMIT 1;",
            tuples_only=True,
            timeout=10,
        )
        if proc is not None and proc.returncode == 0:
            raw_amt = (proc.stdout or "").strip()
            if raw_amt:
                expected_amount_cents = int(raw_amt)
    except Exception:
        pass

    _, msgs = get_sms_transcript(api_url, org_id, customer, token)
    ids_before_payment = [m.id for m in msgs if m.id]

    if not base.send_square_payment_webhook(lead_id, booking_intent_id, expected_amount_cents):
        raise RuntimeError("Square webhook failed")

    # Confirmation SMS
    confirm = wait_for_new_message(api_url, org_id, customer, token, since_ids=ids_before_payment, kind="payment_confirmation", timeout_s=240.0)
    if "Payment" not in confirm.body and "payment" not in confirm.body:
        raise RuntimeError(f"Unexpected confirmation message: {confirm.body!r}")

    # Priority booking verification (DB signals)
    def query_row_json(sql: str, description: str) -> Dict[str, Any]:
        proc = base.run_psql(sql, tuples_only=True, timeout=10)
        if proc is None:
            raise RuntimeError(f"psql not available (and docker compose fallback not found); cannot verify: {description}")
        if proc.returncode != 0:
            raise RuntimeError(f"DB query failed for {description}: {proc.stderr[:300]}")
        raw = (proc.stdout or "").strip()
        if not raw:
            raise RuntimeError(f"DB query returned no rows for {description}")
        try:
            return json.loads(raw)
        except Exception as e:
            raise RuntimeError(f"Failed to parse DB JSON for {description}: {raw[:200]}") from e

    lead_row = query_row_json(
        f"SELECT row_to_json(t) FROM (SELECT deposit_status, priority_level FROM leads WHERE phone = '{customer}' ORDER BY created_at DESC LIMIT 1) t;",
        "lead deposit_status/priority_level",
    )
    if (lead_row.get("deposit_status") or "").strip() != "paid":
        raise RuntimeError(f"Expected leads.deposit_status='paid', got: {lead_row.get('deposit_status')!r}")
    if (lead_row.get("priority_level") or "").strip() != "priority":
        raise RuntimeError(f"Expected leads.priority_level='priority', got: {lead_row.get('priority_level')!r}")

    pay_row = query_row_json(
        f"SELECT row_to_json(t) FROM (SELECT status, amount_cents, provider_ref FROM payments WHERE id = '{booking_intent_id}' LIMIT 1) t;",
        "payment status/amount/provider_ref",
    )
    if (pay_row.get("status") or "").strip() != "succeeded":
        raise RuntimeError(f"Expected payments.status='succeeded', got: {pay_row.get('status')!r}")
    if int(pay_row.get("amount_cents") or 0) != expected_amount_cents:
        raise RuntimeError(f"Expected payments.amount_cents={expected_amount_cents}, got: {pay_row.get('amount_cents')!r}")
    if not (pay_row.get("provider_ref") or "").strip():
        raise RuntimeError("Expected payments.provider_ref to be set after Square webhook")

    booking_row = query_row_json(
        f"SELECT row_to_json(t) FROM (SELECT status, confirmed_at, scheduled_for FROM bookings WHERE org_id = '{org_id}' AND lead_id = '{lead_id}' ORDER BY created_at DESC LIMIT 1) t;",
        "booking status/confirmed_at",
    )
    if (booking_row.get("status") or "").strip() != "confirmed":
        raise RuntimeError(f"Expected bookings.status='confirmed', got: {booking_row.get('status')!r}")
    if booking_row.get("confirmed_at") in (None, ""):
        raise RuntimeError(f"Expected bookings.confirmed_at to be set, got: {booking_row.get('confirmed_at')!r}")

    out = {
        "org_id": org_id,
        "customer_phone": customer,
        "clinic_phone": clinic,
        "lead_id": lead_id,
        "payment_id": booking_intent_id,
        "timings": timings,
    }
    (artifacts_dir / "timings.json").write_text(json.dumps(out, indent=2), encoding="utf-8")
    return out


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--api-url", default=os.getenv("API_URL", "http://localhost:8082"))
    parser.add_argument("--headed", action="store_true", help="Run Playwright headed (debug)")
    args = parser.parse_args()

    api_url = args.api_url.rstrip("/")
    os.environ["API_URL"] = api_url

    project_root = Path(__file__).resolve().parents[1]
    os.chdir(project_root)

    run_id = now_run_id()
    videos_dir = project_root / "tmp" / "e2e_videos"
    artifacts_dir = project_root / "tmp" / "e2e_artifacts" / run_id
    mkdirp(videos_dir)
    mkdirp(artifacts_dir)

    # Load base script (also loads .env via its best-effort loader).
    import e2e_full_flow as base

    # Required envs for this suite (no secrets printed).
    require_env("ADMIN_JWT_SECRET")
    require_env("TELNYX_WEBHOOK_SECRET")
    require_env("TEST_CUSTOMER_PHONE")
    require_env("TEST_CLINIC_PHONE")

    sms_provider = (os.getenv("SMS_PROVIDER") or "").strip().lower()
    if sms_provider != "telnyx":
        raise RuntimeError(f"SMS_PROVIDER must be 'telnyx' for this E2E (got {sms_provider!r})")

    token = make_admin_jwt(require_env("ADMIN_JWT_SECRET"))

    # Seed knowledge and validate persistence (best effort via existing checks).
    if not base.check_health():
        raise RuntimeError(f"API not healthy at {api_url}")
    if not base.seed_knowledge():
        raise RuntimeError("Knowledge seeding failed (check /knowledge endpoint, Redis, and API logs)")
    if not base.verify_rag_knowledge():
        raise RuntimeError("RAG verification failed after seeding knowledge")

    # Compliance/guardrail suite (no video).
    run_compliance_suite(api_url, token)

    # Video run (happy path).
    from playwright.sync_api import sync_playwright

    phone_sim_url = (
        f"{api_url}/admin/e2e/phone-simulator"
        f"?orgID={quote(base.TEST_ORG_ID)}"
        f"&phone={quote(base.TEST_CUSTOMER_PHONE, safe='')}"
        f"&clinic={quote(base.TEST_CLINIC_PHONE, safe='')}"
        f"&poll_ms=800"
    )

    console_lines: List[str] = []
    video_path: Optional[str] = None

    page = None
    context = None
    browser = None
    video = None
    info: Optional[Dict[str, Any]] = None

    try:
        with sync_playwright() as p:
            browser = p.chromium.launch(headless=not args.headed)
            context = browser.new_context(
                viewport={"width": 430, "height": 932},
                record_video_dir=str(videos_dir),
                record_video_size={"width": 430, "height": 932},
                extra_http_headers={"Authorization": f"Bearer {token}"},
            )
            page = context.new_page()
            video = page.video

            def on_console(msg) -> None:
                try:
                    console_lines.append(f"[{msg.type}] {msg.text}")
                except Exception:
                    pass

            page.on("console", on_console)
            page.goto(phone_sim_url, wait_until="domcontentloaded", timeout=60_000)

            info = run_happy_path(api_url, token, artifacts_dir=artifacts_dir)
    except Exception:
        if page is not None:
            try:
                page.screenshot(path=str(artifacts_dir / "failure.png"), full_page=True)
            except Exception:
                pass
            try:
                (artifacts_dir / "failure.html").write_text(page.content(), encoding="utf-8")
            except Exception:
                pass
        (artifacts_dir / "console.log").write_text("\n".join(console_lines), encoding="utf-8")
        (artifacts_dir / "error.txt").write_text(traceback.format_exc(), encoding="utf-8")
        raise
    finally:
        try:
            if page is not None:
                page.close()
        except Exception:
            pass
        try:
            if context is not None:
                context.close()
        except Exception:
            pass
        try:
            if browser is not None:
                browser.close()
        except Exception:
            pass

    if video is not None:
        video_path = video.path()

    (artifacts_dir / "console.log").write_text("\n".join(console_lines), encoding="utf-8")
    if info is not None:
        (artifacts_dir / "result.json").write_text(json.dumps(info, indent=2), encoding="utf-8")

    if not video_path:
        raise RuntimeError("Playwright did not produce a video file")

    print(video_path)
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except KeyboardInterrupt:
        raise SystemExit(130)
