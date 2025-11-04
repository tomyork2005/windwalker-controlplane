package pgx

import (
	"context"
	"errors"

	"control-plane/internal/domain"
	"control-plane/internal/storage"
	"control-plane/internal/storage/dao"

	"github.com/jackc/pgx/v5"
)

func (s *Storage) UpsertUser(ctx context.Context, uuid string, username string) (*domain.User, error) {
	const query = `
        INSERT INTO users (id, tg_username)
        VALUES ($1, $2)
        ON CONFLICT (tg_username) DO UPDATE
            SET tg_username = EXCLUDED.tg_username
        RETURNING id;
    `

	var id string
	err := s.getExecutor(ctx).QueryRow(ctx, query, uuid, username).Scan(&id)
	if err != nil {
		return nil, err
	}

	return &domain.User{
		ID:       id,
		Username: username,
	}, nil
}

func (s *Storage) GetPlanByID(ctx context.Context, planID string) (*domain.Plan, error) {
	const query = `
		SELECT id, name, region, protocol, amount, currency, archived
		FROM plans 
		WHERE id = $1;
    `

	var planDao dao.PlanDAO
	err := s.getExecutor(ctx).QueryRow(ctx, query, planID).Scan(
		&planDao.ID,
		&planDao.Name,
		&planDao.Region,
		&planDao.Protocol,
		&planDao.Amount,
		&planDao.Currency,
		&planDao.Archived)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, storage.ErrNotFound
		}
		return nil, err
	}

	domainPlan, err := dao.FromDaoPlanToDomainPlan(planDao)
	if err != nil {
		return nil, err
	}

	return &domainPlan, nil
}

func (s *Storage) CreateInvoice(ctx context.Context, invoice domain.Invoice) error {
	const query = `
	INSERT INTO invoices (id, user_id, plan_id, payment_provider, amount, currency, status, checkout_url, expires_at)
	values ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

	_, err := s.getExecutor(ctx).Exec(ctx, query,
		invoice.ID,
		invoice.UserID,
		invoice.PlanID,
		invoice.PaymentProvider,
		invoice.Money.Amount,
		invoice.Money.Curr,
		invoice.Status,
		invoice.CheckoutURL,
		invoice.ExpiresAt.Unix())
	if err != nil {
		return err
	}

	return nil
}
