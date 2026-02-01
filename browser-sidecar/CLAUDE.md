# Browser Sidecar - Claude Instructions

> Playwright-based web scraper for medical spa booking availability.

---

## What This Does

Scrapes appointment availability from booking platforms (primarily Moxie):
1. Navigate to booking URL
2. Handle multi-step service/provider selection
3. Navigate calendar to target date
4. Extract available time slots

---

## Project Structure
```
browser-sidecar/
├── src/
│   ├── index.ts      # Entry point
│   ├── scraper.ts    # Core AvailabilityScraper class
│   ├── server.ts     # HTTP server exposing scraper
│   ├── types.ts      # TypeScript interfaces
│   └── logger.ts     # Logging utility
├── tests/            # Integration tests (Jest + Playwright)
└── debug-screenshots/ # Temporary debug images (DO NOT READ)
```

---

## Commands
```bash
npm install           # Install dependencies
npm test              # Run Jest tests
npm run build         # Compile TypeScript to dist/
DEBUG_SCRAPER=true npm test  # Run with screenshot debugging
```

---

## CRITICAL RULES

1. **NEVER read files in these folders:**
   - `debug-screenshots/` — temporary PNGs, delete after use
   - `node_modules/`
   - `dist/`

2. **Screenshot policy:**
   - Screenshots are for debugging ONLY
   - Generate → Use → Delete immediately
   - Never commit screenshots to git

3. **Stay focused:**
   - Only read/modify files in `src/` and `tests/`
   - Do NOT touch parent directories (cmd, infra, web, etc.)

---

## Testing Focus

When writing integration tests, cover:
- [ ] Browser initialization/cleanup
- [ ] Moxie service selection flow (multi-step)
- [ ] Calendar navigation (month changes, day clicks)
- [ ] Time slot extraction (available vs disabled)
- [ ] Error handling (timeouts, navigation failures)
- [ ] Retry logic
- [ ] Platform detection (Moxie vs generic)

---

## Key Types (from types.ts)

- `AvailabilityRequest` — input: bookingUrl, date, timeout
- `AvailabilityResponse` — output: success, slots[], error?
- `TimeSlot` — { time: string, available: boolean }
- `ScraperConfig` — headless, timeout, retries, userAgent