package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/wolfman30/medspa-ai-platform/internal/events"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
)

// handleDeliveryStatus processes a Telnyx delivery receipt and updates the
// message status in the database.
func (h *TelnyxWebhookHandler) handleDeliveryStatus(ctx context.Context, evt telnyxEvent) error {
	var payload telnyxDeliveryPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return fmt.Errorf("decode delivery payload: %w", err)
	}
	providerID, status, errorReason := normalizeTelnyxDelivery(payload)
	if providerID == "" {
		return fmt.Errorf("delivery payload missing message id")
	}
	if status == "" {
		status = "unknown"
	}
	var deliveredAt, failedAt *time.Time
	lowerStatus := strings.ToLower(status)
	switch {
	case lowerStatus == "delivered":
		deliveredAt = &evt.OccurredAt
	case lowerStatus == "undelivered" || strings.Contains(lowerStatus, "fail"):
		failedAt = &evt.OccurredAt
	}
	if err := h.store.UpdateMessageStatus(ctx, providerID, status, deliveredAt, failedAt); err != nil {
		return fmt.Errorf("update message status: %w", err)
	}
	if h.convStore != nil {
		if updater, ok := h.convStore.(conversationStatusUpdater); ok {
			if err := updater.UpdateMessageStatusByProviderID(ctx, providerID, status, errorReason); err != nil {
				h.logger.Warn("failed to update conversation message status", "error", err, "provider_message_id", providerID)
			}
		}
	}
	if h.metrics != nil {
		h.metrics.ObserveInbound(evt.EventType, status)
	}
	return nil
}

// normalizeTelnyxDelivery extracts the provider message ID, status, and error
// reason from a Telnyx delivery payload.
func normalizeTelnyxDelivery(payload telnyxDeliveryPayload) (providerID, status, errorReason string) {
	providerID = strings.TrimSpace(payload.MessageID)
	if providerID == "" {
		providerID = strings.TrimSpace(payload.ID)
	}
	status = strings.TrimSpace(payload.Status)
	if status == "" && len(payload.To) > 0 {
		status = strings.TrimSpace(payload.To[0].Status)
	}
	if len(payload.Errors) > 0 {
		err := payload.Errors[0]
		errorReason = strings.TrimSpace(err.Detail)
		if errorReason == "" {
			errorReason = strings.TrimSpace(err.Title)
		}
		if errorReason == "" {
			errorReason = strings.TrimSpace(err.Code)
		}
	}
	return providerID, status, errorReason
}

// handleHostedOrder processes a hosted number order lifecycle event,
// persisting the order status and emitting an activation event when applicable.
func (h *TelnyxWebhookHandler) handleHostedOrder(ctx context.Context, evt telnyxEvent) error {
	var payload telnyxHostedPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return fmt.Errorf("decode hosted payload: %w", err)
	}
	clinicID, err := uuid.Parse(payload.ClinicID)
	if err != nil {
		return fmt.Errorf("clinic id parse: %w", err)
	}
	tx, err := h.store.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin hosted tx: %w", err)
	}
	defer tx.Rollback(ctx)
	record := messaging.HostedOrderRecord{
		ClinicID:        clinicID,
		E164Number:      payload.PhoneNumber,
		Status:          payload.Status,
		LastError:       payload.LastError,
		ProviderOrderID: payload.ID,
	}
	if err := h.store.UpsertHostedOrder(ctx, tx, record); err != nil {
		return fmt.Errorf("persist hosted order: %w", err)
	}
	if strings.EqualFold(payload.Status, "activated") {
		activated := events.HostedOrderActivatedV1{
			OrderID:     payload.ID,
			ClinicID:    clinicID.String(),
			E164Number:  payload.PhoneNumber,
			ActivatedAt: evt.OccurredAt,
		}
		if _, err := events.AppendCanonicalEvent(ctx, tx, "clinic:"+clinicID.String(), evt.ID, activated); err != nil {
			return fmt.Errorf("append activation event: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit hosted tx: %w", err)
	}
	if h.metrics != nil {
		h.metrics.ObserveInbound(evt.EventType, payload.Status)
	}
	return nil
}

// isDuplicateProviderMessage checks whether a Postgres error indicates a
// unique constraint violation on the provider_message_id column.
func isDuplicateProviderMessage(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	if pgErr.Code != "23505" {
		return false
	}
	if strings.TrimSpace(pgErr.ConstraintName) == "" {
		return true
	}
	return strings.Contains(pgErr.ConstraintName, "provider_message")
}
