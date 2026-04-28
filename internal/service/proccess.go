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
	GetInvoiceByProviderOrder(ctx context.Context, providerName, providerOrderID string) (*model.Invoice, error)
	GetPlanByID(ctx context.Context, planID string) (*model.Plan, error)
	UpdateInvoice(ctx context.Context, invoice *model.Invoice) error
	CreateSubscription(ctx context.Context, sub *model.Subscription) error

	GetSubscriptionByIDForUpdate(ctx context.Context, id string) (*model.SubscriptionWithPlan, error)
	ExtendSubscriptionEndAt(ctx context.Context, id string, newEndAt time.Time) error

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
		return err
	}

	if callback.PaymentOrderID == "" {
		return fmt.Errorf("callback %s has empty provider order id", callback.ProviderName)
	}

	return s.storage.WithTx(ctx, func(ctx context.Context) error {
		inv, err := s.storage.GetInvoiceByProviderOrder(ctx, callback.ProviderName, callback.PaymentOrderID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil
			}
			return err
		}

		if inv.Status == model.SuccessInvoiceStatus {
			return nil
		}

		if callback.Status != model.SuccessInvoiceStatus {
			inv.Status = callback.Status
			return s.storage.UpdateInvoice(ctx, inv)
		}

		inv.Status = model.SuccessInvoiceStatus
		inv.PaidAt = callback.PaidAt
		if err = s.storage.UpdateInvoice(ctx, inv); err != nil {
			return err
		}

		paidEvent := model.InvoicePaidNotificationEvent{ChatID: inv.ChatID}
		if err = s.outbox.SaveOutboxEvent(ctx, model.EventTypeInvoicePaidNotification, paidEvent); err != nil {
			return err
		}

		if inv.RenewsSubscriptionID != nil {
			return s.processRenewal(ctx, inv)
		}

		return s.processNewSubscription(ctx, inv)
	})
}

func (s *ProcessService) processNewSubscription(ctx context.Context, inv *model.Invoice) error {
	plan, err := s.storage.GetPlanByID(ctx, inv.PlanID)
	if err != nil {
		return err
	}

	sub := &model.Subscription{
		ID:        uuid.NewString(),
		UserID:    inv.UserID,
		InvoiceID: &inv.ID,
		PlanID:    plan.ID,
		ChatID:    inv.ChatID,
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
	return s.outbox.SaveOutboxEvent(ctx, model.EventTypeSubscriptionActivated, event)
}

func (s *ProcessService) processRenewal(ctx context.Context, inv *model.Invoice) error {
	sub, err := s.storage.GetSubscriptionByIDForUpdate(ctx, *inv.RenewsSubscriptionID)
	if err != nil {
		return err
	}

	if sub.Status != model.ActiveSubscriptionStatus {
		return ErrSubscriptionExpired
	}
	if sub.AgentID == nil || *sub.AgentID == "" {
		return fmt.Errorf("renewal: subscription %s has no agent_id bound", sub.ID)
	}

	// Берём plan по invoice (юзер мог продлить тем же тарифом, но в общем
	// случае продление — это новая оплата того же plan_id, что висит в invoice).
	plan, err := s.storage.GetPlanByID(ctx, inv.PlanID)
	if err != nil {
		return err
	}

	newEnd := sub.EndAt.AddDate(0, 0, int(plan.DurationDays))
	if err := s.storage.ExtendSubscriptionEndAt(ctx, sub.ID, newEnd); err != nil {
		return err
	}

	renewed := model.SubscriptionRenewedEvent{
		SubscriptionID: sub.ID,
		AgentID:        *sub.AgentID,
		UserID:         sub.UserID,
		DriverType:     sub.DriverType,
		NewEndAt:       newEnd,
	}
	if err := s.outbox.SaveOutboxEvent(ctx, model.EventTypeSubscriptionRenewed, renewed); err != nil {
		return err
	}

	notify := model.SubscriptionRenewedNotificationEvent{
		ChatID:   sub.ChatID,
		NewEndAt: newEnd,
	}
	return s.outbox.SaveOutboxEvent(ctx, model.EventTypeSubscriptionRenewedNotification, notify)
}
