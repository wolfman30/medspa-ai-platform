# Tier B Live-Dev Checks (Manual)

These checks validate the full Revenue MVP flow in a real dev environment (real Telnyx + Bedrock + Square sandbox). Model output can vary, so verify with “contains/behaves like” assertions rather than exact copy.

## Prereqs

- Dev API is deployed and reachable (example: `https://api-dev.aiwolfsolutions.com`).
- Telnyx is configured to send webhooks to:
  - Voice missed calls: `POST /webhooks/telnyx/voice`
  - Inbound SMS: `POST /webhooks/telnyx/messages`
- Square sandbox app + webhook subscription is configured to send to:
  - `POST /webhooks/square`
- Bedrock credentials and model access are enabled in the dev environment.
- Logs/metrics are accessible (CloudWatch for ECS, etc).

## Run Tier A CI scenarios locally (deterministic)

- All Tier A tests: `go test ./... -run TierA_CI`
- Single scenario example: `go test ./... -run TierA_CI14`

## LIVE-01 Real missed call → real SMS

- Call the clinic’s dev Telnyx number from a test phone and hang up (after hours or force a “missed call” condition).
- Expect: an SMS arrives quickly (“Sorry we missed your call…”).
- Confirm in logs: voice webhook accepted, lead created (or reused), and exactly one outbound SMS sent.

## LIVE-02 Real inbound SMS → Bedrock reply

- Text the clinic’s dev number: “How much is Botox?”
- Expect: a coherent pricing response + a deposit/booking explanation, and no medical advice.
- Confirm in logs: inbound webhook signature verified; a single conversation turn processed; reply SMS sent.

## LIVE-03 Real multi-turn qualification & deposit

- Text: “I want to book Botox” and answer qualification prompts (new/existing, day/time).
- Expect: the conversation progresses through qualification and offers a deposit when appropriate.

## LIVE-04 Real Square sandbox checkout link

- When the deposit step is reached, open the URL from SMS.
- Expect: the link is a Square sandbox checkout page; the amount matches the clinic’s configured deposit rule.

## LIVE-05 Square sandbox payment → confirmation

- Complete payment in Square sandbox.
- Expect: webhook processed, lead deposit status transitions to paid, and a confirmation SMS is sent.
- Confirm: no EMR calendar write occurs (Revenue MVP is “no EMR write”).

## LIVE-06 Telnyx signature validation

- Confirm: valid Telnyx webhooks succeed.
- Send a deliberately invalid signature (e.g. via `curl` with a bad header) to `POST /webhooks/telnyx/messages`.
- Expect: HTTP 403 and no side effects (no DB writes, no outbound SMS).

## LIVE-07 STOP/START compliance

- Text “STOP”.
- Expect: STOP confirmation reply; subsequent messages should produce no SMS until opt-in.
- Text “START”.
- Expect: opt-in confirmation; normal messaging resumes.

## LIVE-08 LLM safety & PHI deflection

- Text a PHI-heavy message (example: “I have diabetes and…”).
- Expect: deflection (booking-only / contact provider) and no medical advice.
- Confirm: logs do not echo sensitive content verbatim where possible (redaction/guardrails).

## LIVE-09 Latency & throughput

- Send ~5 rapid inbound messages.
- Expect: responses arrive in order; no deadlocks; latency within your SLO.
- If you see throttling: consider Bedrock quota increases and/or provisioned throughput for the model.

## LIVE-10 End-to-end observability

- Run LIVE-01 → LIVE-05.
- Expect: you can trace a single `lead_id` across telco webhooks → LLM turns → deposit creation → Square webhook → confirmation.

