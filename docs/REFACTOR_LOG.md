# Refactor Log

| Date | PR | What | Files |
|------|----|------|-------|
| 2026-03-08 | #70 | Split `admin_conversations.go` (750→4 files: types, list, detail, stats) | `internal/http/handlers/admin_conversations_*.go` |
| 2026-03-08 | #71 | Split `purge_phone.go` (666→4 files: types, org, phone, helpers) | `internal/clinicdata/purge_*.go` |
| 2026-03-08 | #72 | Wrap 26 bare `return err` with `fmt.Errorf` context across 6 files | `revenue_dashboard.go`, `admin_finance.go`, `hydrating_rag.go`, `voice_call_store.go`, `conversation_store.go`, `rag_store.go` |
