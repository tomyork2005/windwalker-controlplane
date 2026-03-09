package service

import (
	"context"
	"control-plane/internal/model"
	store "control-plane/internal/storage"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"time"
)

type ProcessPayment interface {
	VerifyCallback(input model.CallbackInput) (model.CallbackOutput, error)
	CreatePaymentOrder(ctx context.Context, input model.CreateOrderInput) (model.CreateOrderOutput, error)
}

type Outbox interface {
	SaveOutboxEvent(ctx context.Context, eventType string, payload any) error
}

type ProcessStorage interface {
	GetInvoiceForUpdate(ctx context.Context, uuid string) (*model.Invoice, error)
	GetPlanByID(ctx context.Context, planID string) (*model.Plan, error)
	UpdateInvoice(ctx context.Context, invoice *model.Invoice) error
	CreateSubscription(ctx context.Context, sub *model.Subscription) error

	WithTx(ctx context.Context, fn func(ctx context.Context) error) error
}

type ProcessService struct {
	payment ProcessPayment
	storage ProcessStorage
	outbox  Outbox
}

func NewProcessService(payment ProcessPayment, storage ProcessStorage, outbox Outbox) *ProcessService {
	return &ProcessService{
		payment: payment,
		storage: storage,
		outbox:  outbox,
	}
}

func (s *ProcessService) ProcessPaymentCallback(ctx context.Context, req model.CallbackInput) error {
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
		if inv.Status == model.SuccessInvoiceStatus {
			return nil
		}

		if inv.PaymentProvider != callback.ProviderName {
			return fmt.Errorf("provider mismatch: invoice=%s, callback=%s", inv.PaymentProvider, callback.ProviderName)
		}

		inv.Status = model.SuccessInvoiceStatus
		inv.PaidAt = callback.PaidAt
		if err = s.storage.UpdateInvoice(ctx, inv); err != nil {
			return err
		}

		plan, err := s.storage.GetPlanByID(ctx, inv.PlanID)
		if err != nil {
			return err
		}

		sub := &model.Subscription{
			ID:        uuid.NewString(),
			UserID:    inv.UserID,
			InvoiceID: inv.ID,
			PlanID:    plan.ID,
			Status:    model.ActiveSubscriptionStatus,
			StartAt:   time.Now().UTC(),
			EndAt:     time.Now().UTC().AddDate(0, 0, int(plan.DurationDays)),
		}
		if err := s.storage.CreateSubscription(ctx, sub); err != nil {
			return err
		}

		event := model.SubscriptionActivatedEvent{
			UserID:            sub.UserID,
			Region:            plan.Region,
			DriverType:        plan.DriverType,
			SubscribeDuration: time.Hour * time.Duration(24*plan.DurationDays),
			SubscriptionID:    sub.ID,
		}
		if err = s.outbox.SaveOutboxEvent(ctx, model.EventTypeSubscriptionActivated, event); err != nil {
			return err
		}

		return nil
	})
}
