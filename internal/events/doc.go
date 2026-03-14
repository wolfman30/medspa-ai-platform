// Package events provides a transactional outbox for domain events and
// idempotent webhook processing. It decouples event producers (SMS handlers,
// payment webhooks) from downstream consumers by persisting events to
// PostgreSQL before delivery, ensuring at-least-once semantics even if the
// process crashes mid-flight.
//
// Core components:
//   - [OutboxStore]: writes and reads pending events from the outbox table.
//   - [Deliverer]: polls the outbox and invokes a [DeliveryHandler] for each entry.
//   - [ProcessedStore]: deduplicates inbound webhook events using deterministic UUIDs.
//
// Event types (types.go) are versioned structs (e.g. [PaymentSucceededV1])
// serialized as JSON payloads inside the outbox.
package events
