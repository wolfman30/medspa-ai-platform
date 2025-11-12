package messagingworker

import (
	"context"
	"time"

	"github.com/wolfman30/medspa-ai-platform/internal/messaging"
	"github.com/wolfman30/medspa-ai-platform/internal/messaging/telnyxclient"
	"github.com/wolfman30/medspa-ai-platform/pkg/logging"
)

type hostedStore interface {
	PendingHostedOrders(ctx context.Context, limit int) ([]messaging.HostedOrderRecord, error)
	UpsertHostedOrder(ctx context.Context, q messaging.Querier, record messaging.HostedOrderRecord) error
}

type hostedClient interface {
	GetHostedOrder(ctx context.Context, orderID string) (*telnyxclient.HostedOrder, error)
}

// HostedPoller periodically refreshes hosted number orders until activation.
type HostedPoller struct {
	store    hostedStore
	telnyx   hostedClient
	logger   *logging.Logger
	interval time.Duration
	batch    int
}

func NewHostedPoller(store hostedStore, telnyx hostedClient, logger *logging.Logger) *HostedPoller {
	if logger == nil {
		logger = logging.Default()
	}
	return &HostedPoller{
		store:    store,
		telnyx:   telnyx,
		logger:   logger,
		interval: 10 * time.Minute,
		batch:    20,
	}
}

func (p *HostedPoller) WithInterval(d time.Duration) *HostedPoller {
	if d > 0 {
		p.interval = d
	}
	return p
}

func (p *HostedPoller) WithBatchSize(n int) *HostedPoller {
	if n > 0 {
		p.batch = n
	}
	return p
}

func (p *HostedPoller) Run(ctx context.Context) {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	p.drain(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.drain(ctx)
		}
	}
}

func (p *HostedPoller) drain(ctx context.Context) {
	if p.store == nil || p.telnyx == nil {
		return
	}
	orders, err := p.store.PendingHostedOrders(ctx, p.batch)
	if err != nil {
		p.logger.Error("hosted poll fetch failed", "error", err)
		return
	}
	for _, order := range orders {
		resp, err := p.telnyx.GetHostedOrder(ctx, order.ProviderOrderID)
		if err != nil {
			p.logger.Warn("hosted order poll failed", "error", err, "order_id", order.ProviderOrderID)
			continue
		}
		record := messaging.HostedOrderRecord{
			ID:              order.ID,
			ClinicID:        order.ClinicID,
			E164Number:      resp.PhoneNumber,
			Status:          resp.Status,
			LastError:       resp.LastError,
			ProviderOrderID: resp.ID,
		}
		if err := p.store.UpsertHostedOrder(ctx, nil, record); err != nil {
			p.logger.Error("hosted order update failed", "error", err, "order_id", resp.ID)
		}
	}
}
