# Meta/Instagram DM Integration Setup Checklist

**Generated:** 2026-03-14
**Verify Token (proposed):** `8411f9848a4fe12464020178d59233e8928f41ce0307ccd919849e481e09b092`
*(Do NOT store in secrets yet — use this value when configuring the Meta webhook)*

---

## Code Readiness Assessment

### ✅ What's Done
- **Webhook endpoints** registered in router: `GET /webhooks/instagram` (verification) and `POST /webhooks/instagram` (inbound messages)
- **Signature verification** — HMAC SHA-256 validation of `X-Hub-Signature-256` header using app secret
- **Webhook event parsing** — handles `message` and `postback` events from Meta
- **Graph API client** — sends text messages and button templates via `POST /me/messages` (v18.0)
- **Adapter** wired into conversation engine via `Publisher.EnqueueMessage()`
- **Outbound reply** path: `worker_instagram.go` sends replies via `igMessenger.SendReply()`, includes supervisor checks + output guard (leak detection)
- **Transcript logging** for both inbound and outbound messages
- **Lead resolution** via `SimpleLeadResolver` (uses `ig:` prefix on sender ID)
- **Deterministic conversation IDs** (`ig_{orgID}_{senderID}`)
- **Types** fully defined: `WebhookEvent`, `SendRequest`, `SendResponse`, etc.
- **Tests** exist: `adapter_test.go`, `webhook_test.go`, `client_test.go`

### ⚠️ Code Gaps to Fix Before Go-Live

1. **No `OrgResolver` implementation** — The `ResolveByInstagramPageID` interface is defined but no concrete implementation exists outside the instagram package. Need to implement this (likely in `clinic` or a new package) backed by a DB lookup mapping Instagram Page IDs → org IDs.

2. **No DB migration for Instagram identity mapping** — Need a table like `instagram_page_mappings` (page_id → org_id) and potentially `patient_instagram_identities` for the `IdentityStore` interface.

3. **`IdentityStore` interface defined but unused** — `LinkInstagramToPhone`, `FindPatientByInstagramID`, `FindPatientByPhone` are declared in `adapter.go` but never implemented or wired. This is needed for cross-channel patient identity linking (SMS ↔ Instagram).

4. **Graph API version** — Currently hardcoded to `v18.0`. Should verify this is still current (Meta depreciates versions ~2 years after release).

5. **Adapter wiring in `cmd/api/main.go`** — Verify the adapter is actually instantiated and passed to the router config with env vars (`INSTAGRAM_PAGE_ACCESS_TOKEN`, `INSTAGRAM_APP_SECRET`, `INSTAGRAM_VERIFY_TOKEN`).

6. **No ice-breaker / get-started setup** — Consider sending a Graph API call to configure the Get Started button and ice breakers for the Instagram page.

---

## Meta App Registration Steps

### Phase 1: Create Meta App

- [ ] Go to [Meta for Developers](https://developers.facebook.com/) and log in
- [ ] Click **Create App** → Choose **Business** type
- [ ] App name: e.g. "AI Wolf MedSpa" (or client-specific name)
- [ ] Link to a **Business Portfolio** (create one if needed at [business.facebook.com](https://business.facebook.com))
- [ ] Once created, note the **App ID** and **App Secret** (Settings → Basic)

### Phase 2: Add Instagram Messaging Product

- [ ] In the App Dashboard, click **Add Product** → **Instagram** → **Set Up**
- [ ] Under Instagram → **Settings**, connect your Instagram Professional Account
  - The Instagram account must be a **Business** or **Creator** account (not Personal)
  - It must be linked to a Facebook Page
- [ ] Generate a **Page Access Token** with `instagram_manage_messages` permission
  - Go to Graph API Explorer → select your app → select your page → generate token
  - For production: use a **System User** token (never-expiring) via Business Settings → System Users

### Phase 3: Configure Webhook

- [ ] In the App Dashboard → Instagram → **Webhooks**
- [ ] Click **Subscribe to Events**
- [ ] **Callback URL:** `https://your-domain.com/webhooks/instagram`
- [ ] **Verify Token:** `8411f9848a4fe12464020178d59233e8928f41ce0307ccd919849e481e09b092`
- [ ] Subscribe to these webhook fields:
  - `messages` — incoming DMs
  - `messaging_postbacks` — button taps
- [ ] Click **Verify and Save** — Meta will send a GET request to your callback URL

### Phase 4: Set Environment Variables

- [ ] `INSTAGRAM_VERIFY_TOKEN` = the verify token above
- [ ] `INSTAGRAM_APP_SECRET` = App Secret from Settings → Basic
- [ ] `INSTAGRAM_PAGE_ACCESS_TOKEN` = long-lived page access token from Phase 2
- [ ] Deploy the API with these env vars set

### Phase 5: App Review & Permissions

- [ ] Go to **App Review** → **Permissions and Features**
- [ ] Request these permissions:
  - `instagram_manage_messages` — required for sending/receiving DMs
  - `pages_messaging` — required for Messenger/IG messaging
  - `instagram_basic` — basic profile info
- [ ] Prepare for review:
  - [ ] Privacy Policy URL (must be publicly accessible)
  - [ ] Terms of Service URL
  - [ ] App icon and description
  - [ ] **Screencast demo** showing the messaging flow (Meta requires this)
  - [ ] Explain the use case: "AI-powered appointment booking assistant for medical spas"
  - [ ] Business Verification (submit business docs at business.facebook.com if not already verified)
- [ ] Submit for review (typically takes 1-5 business days)

### Phase 6: Go Live

- [ ] Once approved, toggle App Mode from **Development** to **Live**
- [ ] In Development mode, only admins/testers can message. Live mode opens to all users.
- [ ] Implement the `OrgResolver` (code gap #1 above) so multi-tenant routing works
- [ ] Add the Instagram Page ID → org ID mapping to the database
- [ ] Test end-to-end: send a DM to the Instagram account → verify AI responds
- [ ] Monitor logs for any Graph API errors or webhook delivery failures

### Phase 7: Production Hardening

- [ ] Implement `IdentityStore` for cross-channel patient linking (SMS ↔ IG)
- [ ] Set up webhook failure alerts (Meta retries for up to 24 hours, then disables)
- [ ] Consider rate limits: Instagram API has rate limits per page (~200 calls/hour for messaging)
- [ ] Update Graph API version if v18.0 is deprecated
- [ ] Add metrics/dashboards for Instagram message volume

---

## Quick Reference

| Item | Value |
|------|-------|
| Webhook URL | `https://{domain}/webhooks/instagram` |
| Verify Token | `8411f9848a4fe12464020178d59233e8928f41ce0307ccd919849e481e09b092` |
| Graph API Version | v18.0 |
| Required Permissions | `instagram_manage_messages`, `pages_messaging`, `instagram_basic` |
| Meta Docs | https://developers.facebook.com/docs/instagram-platform/instagram-api-with-instagram-login/messaging |
