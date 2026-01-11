# Video Demo Recording - 3 Scenarios

This document describes how to record a video demonstration of the MedSpa AI Platform showing three customer scenarios with a split-screen view.

## Overview

The demo records a video showing:
- **LEFT PANEL**: Patient phone emulator (iPhone-style SMS view)
- **RIGHT PANEL**: Dashboard with live metrics overlay showing:
  - Number of conversations
  - Deposits collected ($)
  - Conversion rate (%)

## The 3 Scenarios

### Scenario 1: Happy Path (Conversion)
1. Patient calls the Telnyx number, hangs up after ~4 rings (missed call)
2. AI sends "Sorry we missed your call" SMS
3. Patient responds with booking inquiry
4. AI conversation qualifies the lead
5. Patient agrees to pay deposit
6. Square checkout link sent
7. Payment completes (simulated)
8. Confirmation SMS sent

**Dashboard impact**: +1 conversation, +$50 deposit, conversion rate increases

### Scenario 2: Billing Dispute (Escalation to Staff)
1. Patient contacts about being overcharged on a recent visit
2. Patient explains: "I was quoted $175 for HydraFacial but charged $225"
3. **AI recognizes billing dispute** requires human intervention
4. AI escalates to staff and reassures patient it will be handled
5. No booking or deposit - this is a service recovery situation

**Dashboard impact**: +1 conversation, $0 deposit, conversion rate decreases
**Key demonstration**: AI knows its limits and defers sensitive issues to humans

### Scenario 3: No Conversion (Declined)
1. Patient asks about services/pricing
2. AI provides information
3. Patient says "I'll think about it" (no booking)
4. Conversation ends without deposit

**Dashboard impact**: +1 conversation, $0 deposit, conversion rate decreases

## Running the Demo

### Prerequisites

1. **API Running**: The MedSpa API must be running with Telnyx configured
2. **Environment Variables** (in `.env`):
   ```bash
   SMS_PROVIDER=telnyx
   ADMIN_JWT_SECRET=your-secret
   TELNYX_WEBHOOK_SECRET=your-telnyx-secret
   TELNYX_API_KEY=your-api-key
   TEST_ORG_ID=11111111-1111-1111-1111-111111111111
   TEST_CLINIC_PHONE=+13304600937  # Your Telnyx number
   ```

3. **Python Dependencies**:
   ```bash
   pip install requests playwright
   playwright install chromium
   ```

### Running

**Windows PowerShell:**
```powershell
# Standard run (headless)
.\scripts\run-video-demo.ps1

# With custom API URL
.\scripts\run-video-demo.ps1 -ApiUrl http://localhost:8082

# Headed mode (visible browser for debugging)
.\scripts\run-video-demo.ps1 -Headed
```

**Direct Python:**
```bash
python scripts/e2e_video_demo.py --api-url http://localhost:8082

# Headed mode
python scripts/e2e_video_demo.py --api-url http://localhost:8082 --headed
```

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `API_URL` | `http://localhost:8082` | API base URL |
| `TEST_ORG_ID` | `11111111-1111-1111-1111-111111111111` | Org ID for demo |
| `TEST_CLINIC_PHONE` | `+13304600937` | Telnyx clinic number |
| `DEMO_PHONE_1` | `+15550001001` | Customer phone for Scenario 1 |
| `DEMO_PHONE_2` | `+15550001002` | Customer phone for Scenario 2 |
| `DEMO_PHONE_3` | `+15550001003` | Customer phone for Scenario 3 |
| `DEMO_SCENARIO_DELAY` | `3.0` | Seconds between scenarios |
| `DEMO_MESSAGE_DELAY` | `2.0` | Seconds after AI reply before next step |
| `DEMO_READING_DELAY` | `2.5` | Seconds customer "reads" before responding |
| `DEMO_AI_WAIT_TIMEOUT` | `120` | Max seconds to wait for AI response |

### Timing Adjustments

For a slower, more readable demo:
```bash
DEMO_SCENARIO_DELAY=5 DEMO_MESSAGE_DELAY=3 python scripts/e2e_video_demo.py
```

For faster recording (less wait time):
```bash
DEMO_SCENARIO_DELAY=1 DEMO_MESSAGE_DELAY=1 python scripts/e2e_video_demo.py
```

## Output Artifacts

### Videos
- Location: `tmp/demo_videos/`
- Format: `.webm` (Playwright default)
- Resolution: 1920x1080

### Debug Artifacts
- Location: `tmp/demo_artifacts/<run_id>/`
- Contents:
  - `results.json` - Scenario results
  - `failure.png` - Screenshot on failure
  - `error.txt` - Error traceback

## Split-Screen Layout

```
+---------------------------+----------------------------------------+
|                           |                                        |
|   PATIENT VIEW            |   CLINIC DASHBOARD                     |
|   (Phone Simulator)       |                                        |
|                           |   [Dashboard iframe or placeholder]    |
|   +-------------------+   |                                        |
|   | iPhone frame      |   |   +----------------------------------+ |
|   |                   |   |   | Conversations | Deposits | Rate  | |
|   | SMS conversation  |   |   |      3        |   $50    |  33%  | |
|   | bubbles           |   |   +----------------------------------+ |
|   |                   |   |                                        |
|   +-------------------+   |                                        |
|                           |                                        |
|   [Scenario 1: Happy]     |                                        |
|                           |                                        |
+---------------------------+----------------------------------------+
```

## Customizing Scenarios

The scenarios are defined in `scripts/e2e_video_demo.py`:

- `run_scenario_1_happy_path()` - Full conversion flow
- `run_scenario_2_billing_escalation()` - Billing dispute escalated to staff
- `run_scenario_3_no_conversion()` - Inquiry without booking

To modify messages or flow, edit these functions directly.

## Troubleshooting

### "API not healthy"
- Ensure the API is running: `make run-api` or `docker compose up`
- Check the API URL is correct

### "TELNYX_WEBHOOK_SECRET is required"
- This is optional for local development - the API skips verification when not set
- For production, set this to your Telnyx webhook signing secret

### "SMS_PROVIDER must be 'telnyx'"
- Set `SMS_PROVIDER=telnyx` in `.env`
- The demo uses Telnyx webhook simulation

### Video not recording
- Ensure Playwright Chromium is installed: `playwright install chromium`
- Check `tmp/demo_artifacts/` for error logs

### AI responses timeout
- Increase `DEMO_AI_WAIT_TIMEOUT` (default 120s)
- Check AWS Bedrock credentials and quota
- Verify knowledge base is seeded

## Converting Video

To convert `.webm` to `.mp4`:
```bash
ffmpeg -i tmp/demo_videos/demo_xxx.webm -c:v libx264 -crf 20 demo.mp4
```
