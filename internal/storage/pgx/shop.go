package pgx

import (
	"context"
	"control-plane/internal/model"
	"errors"
	"fmt"
	"github.com/georgysavva/scany/v2/pgxscan"
)

func (s *Storage) UpsertUser(ctx context.Context, uuid string, username string) (*model.User, error) {
	const query = `
		INSERT INTO users (id, username)
		VALUES ($1, $2)
		ON CONFLICT (username)
		DO UPDATE SET username = EXCLUDED.username
		RETURNING id, username
	`

	var row userRow
	if err := pgxscan.Get(ctx, s.getExecutor(ctx), &row, query, uuid, username); err != nil {
		return nil, fmt.Errorf("upsert user: %w", err)
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
			archived
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
			archived
		FROM plans
		WHERE archived = FALSE
		ORDER BY duration_days ASC, name ASC
	`

	type planRaw struct {
		ID           string `db:"id"`
		Name         string `db:"name"`
		Region       string `db:"region"`
		Protocol     string `db:"protocol"`
		MoneyAmount  int64  `db:"money_amount"`
		MoneyCurr    string `db:"money_currency"`
		DurationDays int64  `db:"duration_days"`
		Archived     bool   `db:"archived"`
	}

	var rows []*planRaw
	if err := pgxscan.Select(ctx, s.getExecutor(ctx), &rows, query); err != nil {
		return nil, err
	}

	plans := make([]*model.Plan, 0, len(rows))
	for _, r := range rows {
		money, err := model.NewMoney(r.MoneyAmount, r.MoneyCurr)
		if err != nil {
			return nil, err
		}

		plans = append(plans, &model.Plan{
			ID:           r.ID,
			Name:         r.Name,
			Region:       r.Region,
			DriverType:   r.Protocol,
			Money:        money,
			DurationDays: r.DurationDays,
			Archived:     r.Archived,
		})
	}

	return plans, nil
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
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`

	_, err := s.getExecutor(ctx).Exec(
		ctx,
		query,
		invoice.ID,
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
	ID       string `db:"id"`
	Username string `db:"username"`
}

func (r *userRow) toModel() *model.User {
	return &model.User{
		ID:       r.ID,
		Username: r.Username,
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
	}, nil
}
