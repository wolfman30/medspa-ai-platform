# ADR: Modular Messaging ACL with Telnyx Worker

## Context

We needed hosted messaging, 10DLC onboarding, STOP/HELP enforcement, Telnyx webhooks, and reliable outbound delivery without rewriting the entire platform. The repo already shipped a Chi API + conversation worker with PostgreSQL and an outbox table, so the goal was to add a bounded “Messaging ACL” slice that layers on top of existing primitives.

## Decision

- Extend the schema via `002_messaging_acl` to add hosted orders, 10DLC artifacts, a richer messages table, unsubscribes, and upgrades to `outbox/processed_events`.
- Keep Telnyx specifics inside `internal/messaging/telnyxclient` and thin HTTP handlers (`internal/http/handlers`), so the rest of the code references clean interfaces.
- Use canonical events + outbox for every messaging write; webhook handlers append `MessageReceived/HostedOrderActivated` events while admin sends emit `MessageSent` events in the same transaction as DB persistence.
- Build a dedicated `cmd/messaging-worker` binary that reuses the store + Telnyx client to poll hosted orders and retry failed outbound sends with capped exponential backoff.
- Add Prometheus metrics, structured request logging, and coverage gating for the new packages to set observability/testing expectations from day one.

## Consequences

**Pros**

- Minimal blast radius: the core API/worker continue to run while messaging routes live under `/admin` and `/webhooks`.
- Telnyx can be swapped out later because callers depend on the narrow interfaces defined in `internal/http/handlers` and `internal/messaging/telnyxclient`.
- Idempotency is preserved through `processed_events` and outbox transactions, so retries (webhooks or worker) remain safe.
- Observability hooks (Prom metrics, structured logs) and the coverage script give SREs/devs confidence in the new surface.

**Cons**

- Operators must run an additional worker deployment and manage new env vars (Telnyx creds, retry settings, quiet hours).
- More migrations/config increases cold-start complexity for new contributors.
- Coverage gating on specific packages means future changes must maintain ≥90% coverage, adding CI friction.

We purposely avoided a larger re-platform: this modular approach delivers Telnyx hosted messaging quickly while keeping a clean path toward future decomposition if/when the messaging ACL grows into its own service.
