package handlers

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
)

type messagingStore interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	InsertMessage(ctx context.Context, q messaging.Querier, rec messaging.MessageRecord) (uuid.UUID, error)
	InsertBrand(ctx context.Context, q messaging.Querier, rec messaging.BrandRecord) error
	InsertCampaign(ctx context.Context, q messaging.Querier, rec messaging.CampaignRecord) error
	UpsertHostedOrder(ctx context.Context, q messaging.Querier, record messaging.HostedOrderRecord) error
	HasInboundMessage(ctx context.Context, clinicID uuid.UUID, from string, to string) (bool, error)
	IsUnsubscribed(ctx context.Context, clinicID uuid.UUID, recipient string) (bool, error)
	InsertUnsubscribe(ctx context.Context, q messaging.Querier, clinicID uuid.UUID, recipient string, source string) error
	DeleteUnsubscribe(ctx context.Context, q messaging.Querier, clinicID uuid.UUID, recipient string) error
	LookupClinicByNumber(ctx context.Context, number string) (uuid.UUID, error)
	UpdateMessageStatus(ctx context.Context, providerMessageID, status string, deliveredAt, failedAt *time.Time) error
	ScheduleRetry(ctx context.Context, q messaging.Querier, id uuid.UUID, status string, nextRetry time.Time) error
	ListRetryCandidates(ctx context.Context, limit int, maxAttempts int) ([]messaging.MessageRecord, error)
	PendingHostedOrders(ctx context.Context, limit int) ([]messaging.HostedOrderRecord, error)
}
