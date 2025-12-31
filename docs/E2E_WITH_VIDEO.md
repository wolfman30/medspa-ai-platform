# E2E + Phone-View Video (Telnyx + Square)

This repo supports an end-to-end automated test that records a mobile phone-view video (Playwright + a built-in iPhone-style chat UI).

Note: physical iPhone screen recording automation isn't feasible from Windows in a repo-local test harness. The practical substitute is a mobile-viewport simulator page + Playwright video capture.

## What It Runs

- Compliance suite (no video): `STOP`/`HELP`/`START` (+ demo-mode `YES` opt-in), opt-out suppression, PCI guardrail (PAN redaction + no dispatch), Telnyx `message_id` idempotency.
- Happy-path (recorded): missed call -> SMS conversation -> deposit link -> Square sandbox webhook -> confirmation -> DB "priority booking" assertions.

## How To Run (Windows PowerShell)

With the API already running:

```powershell
powershell -File scripts/run-e2e-with-video.ps1 -ApiUrl http://localhost:8082
```

Optional (headed browser for debugging):

```powershell
powershell -File scripts/run-e2e-with-video.ps1 -ApiUrl http://localhost:8082 -Headed
```

Direct (without the PS wrapper):

```powershell
python scripts/e2e_with_video.py --api-url http://localhost:8082
```

## Required Env Vars

At minimum (no secrets are written to disk by the runner):

- `ADMIN_JWT_SECRET` (used to mint a short-lived admin JWT locally for admin endpoints)
- `SMS_PROVIDER=telnyx`
- `TELNYX_API_KEY`
- `TELNYX_MESSAGING_PROFILE_ID`
- `TELNYX_WEBHOOK_SECRET` (used to sign simulated Telnyx webhooks)
- `TEST_CUSTOMER_PHONE` (E.164)
- `TEST_CLINIC_PHONE` (E.164; your Telnyx long-code)

Payments:

- Preferred (real Square sandbox): configure Square credentials/OAuth so `/payments/checkout` can create a real checkout link.
- Dev fallback: set `ALLOW_FAKE_PAYMENTS=true` (never for production).

Optional knobs:

- `DEMO_MODE=true` (enables demo-mode compliance behaviors like `YES` -> `START`)
- `TELNYX_FIRST_CONTACT_REPLY` (first-contact auto-reply content; should only be sent once)
- `E2E_MAX_AI_STEP_SECONDS` (default `45`)
- `E2E_FAIL_ON_AI_LATENCY=true|false`
- `E2E_HTTP_TIMEOUT` (default `20`)
- `E2E_STEP_DELAY_SECONDS` (adds a pause between webhook steps to avoid Bedrock throttling; set back to `0` later)

## Artifacts

- Video files: `tmp/e2e_videos/` (Playwright `.webm` by default)
- Debug artifacts per run: `tmp/e2e_artifacts/<run_id>/`
  - `failure.png` / `failure.html` (only on failure)
  - `console.log`
  - `error.txt` (traceback on failure)
  - `timings.json` / `result.json` (on success)

## Phone Simulator UI (for recording)

- UI page: `/admin/e2e/phone-simulator?orgID=<org>&phone=<customer>&clinic=<clinic>&poll_ms=800`
- Transcript JSON (polled by the UI): `GET /admin/clinics/{orgID}/sms/{phone}?limit=500`
