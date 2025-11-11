package core

import (
	"context"
	"control-plane/internal/adapters/agent"
	"control-plane/internal/domain"
	store "control-plane/internal/storage"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"time"
)

type payment interface {
	CreatePaymentOrder(ctx context.Context, input domain.CreateOrderInput) (domain.CreateOrderOutput, error)
	VerifyCallback(input domain.CallbackInput) (domain.CallbackOutput, error)
}

type agentClient interface {
	UpsertUser(ctx context.Context, input agent.UserUpsertInput) error
	RemoveUser(ctx context.Context, input agent.RemoveUserInput) error
}

type storage interface {
	UpsertUser(ctx context.Context, uuid string, username string) (*domain.User, error)

	GetPlanByID(ctx context.Context, planID string) (*domain.Plan, error)
	GetAllPlans(ctx context.Context) ([]domain.Plan, error)

	GetAllPaymentMethods(ctx context.Context) ([]domain.PaymentMethod, error)

	CreateSubscription(ctx context.Context, sub domain.Subscription) error

	GetInvoiceForUpdate(ctx context.Context, invoiceID string) (domain.Invoice, error)
	CreateInvoice(ctx context.Context, invoice domain.Invoice) error
	UpdateInvoice(ctx context.Context, invoice domain.Invoice) error

	WithTx(ctx context.Context, fn func(ctx context.Context) error) error
}

type outbox interface {
	Save(ctx context.Context, eventType string, payload any) error
}

type Service struct {
	agent   agentClient
	payment payment
	storage storage
	outbox  outbox
}

func NewService(agent agentClient, payment payment, storage storage, outbox outbox) *Service {
	return &Service{
		storage: storage,
		agent:   agent,
		payment: payment,
		outbox:  outbox,
	}
}

func (s *Service) ListPaymentMethods(ctx context.Context) ([]domain.PaymentMethod, error) {
	methods, err := s.storage.GetAllPaymentMethods(ctx)
	if err != nil {
		return nil, err
	}

	return methods, nil
}

func (s *Service) ListPlans(ctx context.Context) ([]domain.Plan, error) {
	plans, err := s.storage.GetAllPlans(ctx)
	if err != nil {
		return nil, err
	}

	return plans, nil
}

func (s *Service) CreateInvoice(ctx context.Context, planID string, username string, methodID string) (*domain.Invoice, error) {
	user, err := s.storage.UpsertUser(ctx, uuid.NewString(), username)
	if err != nil {
		return nil, err
	}

	plan, err := s.storage.GetPlanByID(ctx, planID)
	if err != nil {
		return nil, err
	}

	invoiceID := uuid.New().String()
	output, err := s.payment.CreatePaymentOrder(ctx, domain.CreateOrderInput{
		InvoiceID: invoiceID,
		Money:     plan.Money,
		MethodID:  methodID,
	})
	if err != nil {
		return nil, err
	}

	invoice := domain.Invoice{
		ID:              invoiceID,
		UserID:          user.ID,
		PlanID:          plan.ID,
		PaymentProvider: output.ProviderName,
		Money:           plan.Money,
		Status:          domain.CreatedInvoiceStatus,
		CheckoutURL:     output.RedirectURL,
		CreatedAt:       time.Now().UTC(),
		ExpiresAt:       output.ExpiredAt,
	}
	err = s.storage.CreateInvoice(ctx, invoice)
	if err != nil {
		return nil, err
	}

	return &invoice, err
}

func (s *Service) ProcessPaymentCallback(ctx context.Context, req domain.CallbackInput) error {
	callback, err := s.payment.VerifyCallback(req)
	if err != nil {
		// todo route errors  --> response to provider
		return err
	}

	return s.storage.WithTx(ctx, func(ctx context.Context) error {
		inv, err := s.storage.GetInvoiceForUpdate(ctx, callback.InvoiceID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil
			}
			return err
		}

		// idempotency, if provider retry nothing to do
		if inv.Status == domain.SuccessInvoiceStatus {
			return nil
		}

		if inv.PaymentProvider != callback.ProviderName {
			return fmt.Errorf("provider mismatch: invoice=%s, callback=%s", inv.PaymentProvider, callback.ProviderName)
		}

		inv.Status = domain.SuccessInvoiceStatus
		inv.PaidAt = callback.PaidAt
		if err = s.storage.UpdateInvoice(ctx, inv); err != nil {
			return err
		}

		plan, err := s.storage.GetPlanByID(ctx, inv.PlanID)
		if err != nil {
			return err
		}

		sub := domain.Subscription{
			ID:        uuid.NewString(),
			UserID:    inv.UserID,
			InvoiceID: inv.ID,
			PlanID:    plan.ID,
			Status:    domain.ActiveSubscriptionStatus,
			StartAt:   time.Now().UTC(),
			EndAt:     time.Now().UTC().AddDate(0, 0, int(plan.DurationDays)),
		}
		if err := s.storage.CreateSubscription(ctx, sub); err != nil {
			return err
		}

		event := domain.SubscriptionActivatedEvent{
			SubscriptionID: sub.ID,
			UserID:         sub.UserID,
			PlanID:         plan.ID,
			InvoiceID:      inv.ID,
		}
		if err := s.outbox.Save(ctx, domain.EventTypeSubscriptionActivated, event); err != nil {
			return err
		}

		return nil
	})
}
