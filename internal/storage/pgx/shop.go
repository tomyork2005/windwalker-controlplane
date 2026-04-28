package pgx

import (
	"context"
	"control-plane/internal/model"
	"control-plane/internal/storage"
	"errors"
	"fmt"
	"time"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/google/uuid"
)

func (s *Storage) UpsertUserByTelegramID(ctx context.Context, telegramID int64, username string) (*model.User, error) {
	if username == "" {
		username = fmt.Sprintf("tg_%d", telegramID)
	}

	const query = `
		INSERT INTO users (id, username, telegram_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (telegram_id) WHERE telegram_id IS NOT NULL
		DO UPDATE SET username = EXCLUDED.username
		RETURNING id, username, telegram_id
	`

	var row userRow
	if err := pgxscan.Get(ctx, s.getExecutor(ctx), &row, query, uuid.NewString(), username, telegramID); err != nil {
		return nil, fmt.Errorf("upsert user by telegram id: %w", err)
	}

	return row.toModel(), nil
}

func (s *Storage) GetPlanByID(ctx context.Context, planID string) (*model.Plan, error) {
	const query = `
		SELECT
			id,
			name,
			region,
			protocol,
			money_amount,
			money_currency,
			duration_days,
			archived,
			is_trial
		FROM plans
		WHERE id = $1
		LIMIT 1
	`

	var row planRow
	if err := pgxscan.Get(ctx, s.getExecutor(ctx), &row, query, planID); err != nil {
		return nil, fmt.Errorf("get plan by id: %w", err)
	}

	plan, err := row.toModel()
	if err != nil {
		return nil, fmt.Errorf("map plan row: %w", err)
	}

	return plan, nil
}

func (s *Storage) GetAllPlans(ctx context.Context) ([]*model.Plan, error) {
	const query = `
		SELECT
			id,
			name,
			region,
			protocol,
			money_amount,
			money_currency,
			duration_days,
			archived,
			is_trial
		FROM plans
		WHERE archived = FALSE AND is_trial = FALSE
		ORDER BY duration_days ASC, name ASC
	`

	var rows []*planRow
	if err := pgxscan.Select(ctx, s.getExecutor(ctx), &rows, query); err != nil {
		return nil, err
	}

	plans := make([]*model.Plan, 0, len(rows))
	for _, r := range rows {
		plan, err := r.toModel()
		if err != nil {
			return nil, err
		}
		plans = append(plans, plan)
	}

	return plans, nil
}

func (s *Storage) GetAllTrialPlans(ctx context.Context) ([]*model.Plan, error) {
	const query = `
		SELECT
			id,
			name,
			region,
			protocol,
			money_amount,
			money_currency,
			duration_days,
			archived,
			is_trial
		FROM plans
		WHERE is_trial = TRUE AND archived = FALSE
		ORDER BY region ASC, protocol ASC
	`

	var rows []*planRow
	if err := pgxscan.Select(ctx, s.getExecutor(ctx), &rows, query); err != nil {
		return nil, fmt.Errorf("get all trial plans: %w", err)
	}

	plans := make([]*model.Plan, 0, len(rows))
	for _, r := range rows {
		plan, err := r.toModel()
		if err != nil {
			return nil, err
		}
		plans = append(plans, plan)
	}

	return plans, nil
}

func (s *Storage) GetTrialPlanByRegion(ctx context.Context, region string) (*model.Plan, error) {
	const query = `
		SELECT
			id,
			name,
			region,
			protocol,
			money_amount,
			money_currency,
			duration_days,
			archived,
			is_trial
		FROM plans
		WHERE region = $1 AND is_trial = TRUE AND archived = FALSE
		ORDER BY protocol ASC
		LIMIT 1
	`

	var row planRow
	if err := pgxscan.Get(ctx, s.getExecutor(ctx), &row, query, region); err != nil {
		if pgxscan.NotFound(err) {
			return nil, storage.ErrNotFound
		}
		return nil, fmt.Errorf("get trial plan by region: %w", err)
	}

	plan, err := row.toModel()
	if err != nil {
		return nil, fmt.Errorf("map plan row: %w", err)
	}

	return plan, nil
}

func (s *Storage) HasUsedTrial(ctx context.Context, userID string) (bool, error) {
	const query = `
		SELECT EXISTS(SELECT 1 FROM subscriptions WHERE user_id = $1 AND is_trial = TRUE)
	`

	var exists bool
	if err := pgxscan.Get(ctx, s.getExecutor(ctx), &exists, query, userID); err != nil {
		return false, fmt.Errorf("has used trial: %w", err)
	}

	return exists, nil
}

func (s *Storage) GetActiveSubscriptionByUser(ctx context.Context, userID string) (*model.SubscriptionWithPlan, error) {
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
		WHERE s.user_id = $1 AND s.status = 'active' AND s.end_at > now()
		ORDER BY s.end_at DESC
		LIMIT 1
	`

	var row subscriptionWithPlanRow
	if err := pgxscan.Get(ctx, s.getExecutor(ctx), &row, query, userID); err != nil {
		if pgxscan.NotFound(err) {
			return nil, storage.ErrNotFound
		}
		return nil, fmt.Errorf("get active subscription by user: %w", err)
	}

	return row.toModel(), nil
}

func (s *Storage) GetAllPaymentMethods(ctx context.Context) ([]*model.PaymentMethod, error) {
	const query = `
		SELECT
			id,
			name
		FROM payment_methods
		ORDER BY name ASC
	`

	var methods []*model.PaymentMethod
	if err := pgxscan.Select(ctx, s.getExecutor(ctx), &methods, query); err != nil {
		return nil, fmt.Errorf("get all payment methods: %w", err)
	}

	return methods, nil
}

func (s *Storage) CreateInvoice(ctx context.Context, invoice *model.Invoice) error {
	if invoice == nil {
		return errors.New("invoice is nil")
	}

	const query = `
		INSERT INTO invoices (
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
			expires_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`

	var providerOrderID any
	if invoice.ProviderOrderID != "" {
		providerOrderID = invoice.ProviderOrderID
	}

	_, err := s.getExecutor(ctx).Exec(
		ctx,
		query,
		invoice.ID,
		providerOrderID,
		invoice.UserID,
		invoice.PlanID,
		invoice.ChatID,
		invoice.PaymentProvider,
		invoice.Money.Amount,
		invoice.Money.Curr,
		invoice.Status,
		invoice.CheckoutURL,
		invoice.CreatedAt,
		invoice.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("create invoice: %w", err)
	}

	return nil
}

// DAO

type userRow struct {
	ID         string `db:"id"`
	Username   string `db:"username"`
	TelegramID *int64 `db:"telegram_id"`
}

func (r *userRow) toModel() *model.User {
	return &model.User{
		ID:         r.ID,
		Username:   r.Username,
		TelegramID: r.TelegramID,
	}
}

type planRow struct {
	ID           string `db:"id"`
	Name         string `db:"name"`
	Region       string `db:"region"`
	Protocol     string `db:"protocol"`
	MoneyAmount  int64  `db:"money_amount"`
	MoneyCurr    string `db:"money_currency"`
	DurationDays int64  `db:"duration_days"`
	Archived     bool   `db:"archived"`
	IsTrial      bool   `db:"is_trial"`
}

func (r *planRow) toModel() (*model.Plan, error) {
	money, err := model.NewMoney(r.MoneyAmount, r.MoneyCurr)
	if err != nil {
		return nil, fmt.Errorf("build plan money: %w", err)
	}

	return &model.Plan{
		ID:           r.ID,
		Name:         r.Name,
		Region:       r.Region,
		DriverType:   r.Protocol,
		Money:        money,
		DurationDays: r.DurationDays,
		Archived:     r.Archived,
		IsTrial:      r.IsTrial,
	}, nil
}

type subscriptionWithPlanRow struct {
	ID           string     `db:"id"`
	UserID       string     `db:"user_id"`
	InvoiceID    *string    `db:"invoice_id"`
	PlanID       string     `db:"plan_id"`
	AgentID      *string    `db:"agent_id"`
	ChatID       int64      `db:"chat_id"`
	Status       string     `db:"status"`
	IsTrial      bool       `db:"is_trial"`
	StartAt      time.Time  `db:"start_at"`
	EndAt        time.Time  `db:"end_at"`
	Creds        *string    `db:"creds"`
	CredsReadyAt *time.Time `db:"creds_ready_at"`
	PlanName     string     `db:"plan_name"`
	PlanRegion   string     `db:"plan_region"`
	PlanProtocol string     `db:"plan_protocol"`
	PlanDuration int64      `db:"plan_duration_days"`
}

func (r *subscriptionWithPlanRow) toModel() *model.SubscriptionWithPlan {
	return &model.SubscriptionWithPlan{
		Subscription: model.Subscription{
			ID:           r.ID,
			UserID:       r.UserID,
			InvoiceID:    r.InvoiceID,
			PlanID:       r.PlanID,
			AgentID:      r.AgentID,
			ChatID:       r.ChatID,
			Status:       model.SubscriptionStatus(r.Status),
			IsTrial:      r.IsTrial,
			StartAt:      r.StartAt,
			EndAt:        r.EndAt,
			Creds:        r.Creds,
			CredsReadyAt: r.CredsReadyAt,
		},
		PlanName:     r.PlanName,
		Region:       r.PlanRegion,
		DriverType:   r.PlanProtocol,
		DurationDays: r.PlanDuration,
	}
}
