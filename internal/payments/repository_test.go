package payments

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	paymentsql "github.com/wolfman30/medspa-ai-platform/internal/payments/sqlc"
)

func TestCreateIntentPersistsScheduledFor(t *testing.T) {
	scheduled := time.Now().Add(3 * time.Hour).UTC()
	querier := &stubPaymentQuerier{}
	repo := NewRepositoryWithQuerier(querier)

	orgID := uuid.New()
	leadID := uuid.New()
	intentID := uuid.New()

	if _, err := repo.CreateIntent(context.Background(), orgID, leadID, "square", intentID, 7500, "pending", &scheduled); err != nil {
		t.Fatalf("CreateIntent returned error: %v", err)
	}

	if querier.lastInsert == nil {
		t.Fatalf("expected InsertPayment to be called")
	}
	if !querier.lastInsert.ScheduledFor.Valid {
		t.Fatalf("expected scheduled_for to be set on payment insert")
	}
	if !querier.lastInsert.ScheduledFor.Time.Equal(scheduled) {
		t.Fatalf("scheduled_for mismatch, got %s want %s", querier.lastInsert.ScheduledFor.Time, scheduled)
	}
}

type stubPaymentQuerier struct {
	lastInsert *paymentsql.InsertPaymentParams
}

func (s *stubPaymentQuerier) InsertPayment(ctx context.Context, arg paymentsql.InsertPaymentParams) (paymentsql.Payment, error) {
	s.lastInsert = &arg
	return paymentsql.Payment{ScheduledFor: arg.ScheduledFor}, nil
}

func (*stubPaymentQuerier) GetPaymentByID(ctx context.Context, id pgtype.UUID) (paymentsql.Payment, error) {
	return paymentsql.Payment{}, nil
}

func (*stubPaymentQuerier) GetPaymentByProviderRef(ctx context.Context, providerRef pgtype.Text) (paymentsql.Payment, error) {
	return paymentsql.Payment{}, nil
}

func (*stubPaymentQuerier) UpdatePaymentStatusByID(ctx context.Context, arg paymentsql.UpdatePaymentStatusByIDParams) (paymentsql.Payment, error) {
	return paymentsql.Payment{}, nil
}

func (*stubPaymentQuerier) UpdatePaymentStatusByProviderRef(ctx context.Context, arg paymentsql.UpdatePaymentStatusByProviderRefParams) (paymentsql.Payment, error) {
	return paymentsql.Payment{}, nil
}

func (*stubPaymentQuerier) GetOpenDepositByOrgAndLead(ctx context.Context, arg paymentsql.GetOpenDepositByOrgAndLeadParams) (paymentsql.Payment, error) {
	return paymentsql.Payment{}, nil
}
