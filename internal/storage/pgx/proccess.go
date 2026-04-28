package pgx

import (
	"context"
	"control-plane/internal/model"
	"control-plane/internal/storage"
	"errors"
	"fmt"
	"github.com/georgysavva/scany/v2/pgxscan"
	"time"
)

func (s *Storage) GetInvoiceForUpdate(ctx context.Context, id string) (*model.Invoice, error) {
	const query = `
		SELECT
			id,
			provider_order_id,
			user_id,
			plan_id,
			chat_id,
			payment_provider,
			money_amount,
			money_currency,
			status,
			checkout_url,
			created_at,
			expires_at,
			paid_at
		FROM invoices
		WHERE id = $1
		FOR UPDATE
	`

	var row invoiceRow
	if err := pgxscan.Get(ctx, s.getExecutor(ctx), &row, query, id); err != nil {
		if pgxscan.NotFound(err) {
			return nil, storage.ErrNotFound
		}
		return nil, fmt.Errorf("get invoice for update: %w", err)
	}

	invoice, err := row.toModel()
	if err != nil {
		return nil, fmt.Errorf("map invoice row: %w", err)
	}

	return invoice, nil
}

func (s *Storage) GetInvoiceByProviderOrder(ctx context.Context, providerName, providerOrderID string) (*model.Invoice, error) {
	const query = `
		SELECT
			id,
			provider_order_id,
			user_id,
			plan_id,
			chat_id,
			payment_provider,
			money_amount,
			money_currency,
			status,
			checkout_url,
			created_at,
			expires_at,
			paid_at
		FROM invoices
		WHERE payment_provider = $1 AND provider_order_id = $2
		FOR UPDATE
	`

	var row invoiceRow
	if err := pgxscan.Get(ctx, s.getExecutor(ctx), &row, query, providerName, providerOrderID); err != nil {
		if pgxscan.NotFound(err) {
			return nil, storage.ErrNotFound
		}
		return nil, fmt.Errorf("get invoice by provider order: %w", err)
	}

	invoice, err := row.toModel()
	if err != nil {
		return nil, fmt.Errorf("map invoice row: %w", err)
	}

	return invoice, nil
}

func (s *Storage) UpdateInvoice(ctx context.Context, invoice *model.Invoice) error {
	if invoice == nil {
		return errors.New("invoice is nil")
	}

	const query = `
		UPDATE invoices
		SET
			status = $2,
			paid_at = $3
		WHERE id = $1
	`

	var paidAt any
	if !invoice.PaidAt.IsZero() {
		paidAt = invoice.PaidAt
	}

	tag, err := s.getExecutor(ctx).Exec(
		ctx,
		query,
		invoice.ID,
		string(invoice.Status),
		paidAt,
	)
	if err != nil {
		return fmt.Errorf("update invoice: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return storage.ErrNotFound
	}

	return nil
}

// ListExpiredActiveSubs locks up to `limit` expired active subscriptions for
// processing in a Cleaner tick. MUST be called inside WithTx — the FOR UPDATE
// lock is held until COMMIT, which is what gives us cross-instance dedup.
// Joins plans to surface driver_type (subscriptions table doesn't carry it).
func (s *Storage) ListExpiredActiveSubs(ctx context.Context, limit int) ([]*model.SubscriptionWithPlan, error) {
	const query = `
		SELECT
			s.id,
			s.user_id,
			s.invoice_id,
			s.plan_id,
			s.agent_id,
			s.chat_id,
			s.status,
			s.is_trial,
			s.start_at,
			s.end_at,
			s.creds,
			s.creds_ready_at,
			p.name           AS plan_name,
			p.region         AS plan_region,
			p.protocol       AS plan_protocol,
			p.duration_days  AS plan_duration_days
		FROM subscriptions s
		JOIN plans p ON p.id = s.plan_id
		WHERE s.status = 'active' AND s.end_at < now()
		ORDER BY s.end_at
		LIMIT $1
		FOR UPDATE OF s SKIP LOCKED
	`

	var rows []*subscriptionWithPlanRow
	if err := pgxscan.Select(ctx, s.getExecutor(ctx), &rows, query, limit); err != nil {
		return nil, fmt.Errorf("list expired active subs: %w", err)
	}

	out := make([]*model.SubscriptionWithPlan, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toModel())
	}

	return out, nil
}

func (s *Storage) MarkSubscriptionInactive(ctx context.Context, id string) error {
	const query = `UPDATE subscriptions SET status = 'inactive' WHERE id = $1`

	tag, err := s.getExecutor(ctx).Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("mark subscription inactive: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return storage.ErrNotFound
	}

	return nil
}

func (s *Storage) CreateSubscription(ctx context.Context, sub *model.Subscription) error {
	const query = `
		INSERT INTO subscriptions (
			id,
			user_id,
			invoice_id,
			plan_id,
			chat_id,
			status,
			is_trial,
			start_at,
			end_at
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9
		)
	`

	_, err := s.getExecutor(ctx).Exec(
		ctx,
		query,
		sub.ID,
		sub.UserID,
		sub.InvoiceID,
		sub.PlanID,
		sub.ChatID,
		string(sub.Status),
		sub.IsTrial,
		sub.StartAt,
		sub.EndAt,
	)
	if err != nil {
		return fmt.Errorf("create subscription: %w", err)
	}

	return nil
}

// DAO

type invoiceRow struct {
	ID              string     `db:"id"`
	ProviderOrderID *string    `db:"provider_order_id"`
	UserID          string     `db:"user_id"`
	PlanID          string     `db:"plan_id"`
	ChatID          int64      `db:"chat_id"`
	PaymentProvider string     `db:"payment_provider"`
	MoneyAmount     int64      `db:"money_amount"`
	MoneyCurr       string     `db:"money_currency"`
	Status          string     `db:"status"`
	CheckoutURL     string     `db:"checkout_url"`
	CreatedAt       time.Time  `db:"created_at"`
	ExpiresAt       time.Time  `db:"expires_at"`
	PaidAt          *time.Time `db:"paid_at"`
}

func (r *invoiceRow) toModel() (*model.Invoice, error) {
	money, err := model.NewMoney(r.MoneyAmount, r.MoneyCurr)
	if err != nil {
		return nil, fmt.Errorf("build invoice money: %w", err)
	}

	invoice := &model.Invoice{
		ID:              r.ID,
		UserID:          r.UserID,
		PlanID:          r.PlanID,
		ChatID:          r.ChatID,
		PaymentProvider: r.PaymentProvider,
		Money:           money,
		Status:          model.InvoiceStatus(r.Status),
		CheckoutURL:     r.CheckoutURL,
		CreatedAt:       r.CreatedAt,
		ExpiresAt:       r.ExpiresAt,
	}

	if r.ProviderOrderID != nil {
		invoice.ProviderOrderID = *r.ProviderOrderID
	}

	if r.PaidAt != nil {
		invoice.PaidAt = *r.PaidAt
	}

	return invoice, nil
}
