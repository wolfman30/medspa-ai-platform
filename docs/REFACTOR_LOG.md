# Refactor Log

| Date | PR | What | Files |
|------|----|------|-------|
| 2026-03-08 | #70 | Split `admin_conversations.go` (750→4 files: types, list, detail, stats) | `internal/http/handlers/admin_conversations_*.go` |
| 2026-03-08 | #71 | Split `purge_phone.go` (666→4 files: types, org, phone, helpers) | `internal/clinicdata/purge_*.go` |
| 2026-03-08 | #72 | Wrap 26 bare `return err` with `fmt.Errorf` context across 6 files | `revenue_dashboard.go`, `admin_finance.go`, `hydrating_rag.go`, `voice_call_store.go`, `conversation_store.go`, `rag_store.go` |
| 2026-03-10 | #80 | Split `router.New` (341-line func, 512-line file) into 4 domain files + 7 sub-helpers | `internal/api/router/routes_{public,admin,portal,tenant}.go` |
| 2026-03-09 | #73 | Split `admin_leads.go` (579→5 files: types, list, detail, update, stats) | `internal/http/handlers/admin_leads_*.go` |
| 2026-03-09 | #74 | Split `oauth.go` (569→5 files: types, token, store, locations, core) | `internal/payments/oauth_*.go` |
| 2026-03-10 | #78 | Extract 7 magic numbers to named constants (pagination, phone digits, upload size, webhook tolerance, label truncation) | `internal/http/handlers/constants.go`, `internal/payments/constants.go` |
| 2026-03-10 | #82 | Add godoc comments to 38 exported symbols across events, bootstrap, and worker packages | `internal/events/*.go`, `internal/bootstrap/*.go`, `internal/worker/messaging/*.go` |
| 2026-03-11 | #83 | Wrap 41 bare `return err` with `fmt.Errorf` context across 22 files (round 2) | 22 files across 12 packages (bootstrap, briefs, channels, clinicdata, conversation, emr, handlers, messaging, onboarding, payments, prospects, support) |
| 2026-03-11 | #85 | Split `archive.go` (531→3 files: types, redact, operations) + extracted `uploadArchive()` helper | `internal/clinicdata/archive*.go` |
| 2026-03-11 | #87 | Split `nextech/client.go` (515→3 files: client, appointments, patients) + extracted named constants | `internal/emr/nextech/{client,appointments,patients}.go` |
| 2026-03-12 | #88 | Split `voice/tools.go` (554→3 files: definitions, availability, handlers) + added error context | `internal/voice/tools_{definitions,availability,handlers}.go` |
| 2026-03-12 | #91 | Add godoc comments for exported prospects types/repository/handler symbols | `internal/prospects/{model,repository,handler}.go` |
| 2026-03-12 | #92 | Add missing Godoc comments for exported symbols in `internal/events` | `internal/events/outbox.go`, `internal/events/processed_store.go`, `internal/events/types.go`, `internal/events/messaging_types.go` |
| 2026-03-12 | #93 | Add godoc comments for exported types in service test and knowledge seeding scripts | `scripts/service-tests/main.go`, `scripts/seed-knowledge/main.go` |
| 2026-03-14 | #99 | Split `system_prompt.go` (627→473+162 lines) into templates and builder logic | `internal/conversation/system_prompt_templates.go`, `internal/conversation/system_prompt.go` |
| 2026-03-14 | #100 | Split `llm_helpers.go` (578→3 files: helpers, content detection, lead helpers) + decompose 140-line `appendContext()` into 6 focused helpers | `internal/conversation/llm_helpers.go`, `llm_content_detection.go`, `llm_lead_helpers.go` |
| 2026-03-14 | #101 | Split `router.go` (551→219 lines) into 4 domain route files (public, admin, portal, tenant) | `internal/api/router/routes_{public,admin,portal,tenant}.go` |
