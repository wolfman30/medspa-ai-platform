# E2E Test Results - Revenue MVP

Use this log to record end-to-end runs across hosted numbers, AI conversation, Square deposits, and webhook confirmations.

## Latest Run
- **Date:** _not run in this session_
- **Environment:** local API + workers
- **Numbers:** Telnyx hosted (`/webhooks/telnyx/*`)
- **Org/Clinic:** demo-radiance-medspa
- **Flow exercised:** missed call → SMS reply → AI conversation → manual checkout link → Square webhook confirmation
- **Outcome:** _pending manual execution_

## How to Run
1) Start API + workers with USE_MEMORY_QUEUE or SQS.  
2) Seed knowledge: `./scripts/seed-knowledge.sh http://localhost:8080 demo-radiance-medspa testdata/demo-clinic-knowledge.json`  
3) Run smoke: `./scripts/test-e2e.sh` (requires `jq`).  
4) Open the checkout URL, pay (Square OAuth per clinic), and watch for `payment_succeeded` outbox events.  
5) Verify:
   - Missed call ack SMS comes from clinic/hosted number.
   - Conversation transcript stored for the caller.
   - Deposit status updated on the lead and payment intent marked `succeeded`.
   - Confirmation SMS sent with clinic-specific from-number.

## Manual Notes
- Capture Square webhook payload and payment IDs here once exercised.
- Record any errors, retries, or missing data so they can be replicated.
