package service

import (
	"context"
	"control-plane/internal/model"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

const trialDuration = 72 * time.Hour

type ShopPayment interface {
	CreatePaymentOrder(ctx context.Context, input model.CreateOrderInput) (model.CreateOrderOutput, error)
	VerifyCallback(input model.CallbackInput) (model.CallbackOutput, error)
}

type ShopOutbox interface {
	SaveOutboxEvent(ctx context.Context, eventType string, payload any) error
}

type ShopStorage interface {
	UpsertUserByTelegramID(ctx context.Context, telegramID int64, username string) (*model.User, error)
	GetAllPlans(ctx context.Context) ([]*model.Plan, error)
	GetAllTrialPlans(ctx context.Context) ([]*model.Plan, error)
	GetPlanByID(ctx context.Context, planID string) (*model.Plan, error)
	GetAllPaymentMethods(ctx context.Context) ([]*model.PaymentMethod, error)
	CreateInvoice(ctx context.Context, invoice *model.Invoice) error
	CreateSubscription(ctx context.Context, sub *model.Subscription) error
	HasUsedTrial(ctx context.Context, userID string) (bool, error)
	GetActiveSubscriptionByUser(ctx context.Context, userID string) (*model.SubscriptionWithPlan, error)

	WithTx(ctx context.Context, fn func(ctx context.Context) error) error
}

type ShopService struct {
	payment ShopPayment
	storage ShopStorage
	outbox  ShopOutbox
}

func NewShopService(payment ShopPayment, storage ShopStorage, outbox ShopOutbox) *ShopService {
	return &ShopService{
		storage: storage,
		payment: payment,
		outbox:  outbox,
	}
}

func (s *ShopService) ListPaymentMethods(ctx context.Context) ([]*model.PaymentMethod, error) {
	return s.storage.GetAllPaymentMethods(ctx)
}

func (s *ShopService) ListPlans(ctx context.Context) ([]*model.Plan, error) {
	return s.storage.GetAllPlans(ctx)
}

func (s *ShopService) ListTrialPlans(ctx context.Context) ([]*model.Plan, error) {
	return s.storage.GetAllTrialPlans(ctx)
}

func (s *ShopService) UpsertUserByTelegramID(ctx context.Context, telegramID int64, username string) (*model.User, error) {
	return s.storage.UpsertUserByTelegramID(ctx, telegramID, username)
}

func (s *ShopService) GetActiveSubscriptionByUser(ctx context.Context, userID string) (*model.SubscriptionWithPlan, error) {
	return s.storage.GetActiveSubscriptionByUser(ctx, userID)
}

func (s *ShopService) HasUsedTrial(ctx context.Context, userID string) (bool, error) {
	return s.storage.HasUsedTrial(ctx, userID)
}

func (s *ShopService) CreateInvoice(ctx context.Context, planID string, telegramID int64, username string, methodID string, chatID int64, renewsSubscriptionID *string) (*model.Invoice, error) {
	user, err := s.storage.UpsertUserByTelegramID(ctx, telegramID, username)
	if err != nil {
		return nil, err
	}

	slog.Info("CreateInvoice user:", "planID:", planID, "methodID:", methodID, "chat_id:", chatID, "renews_subscription_id:", renewsSubscriptionID)

	plan, err := s.storage.GetPlanByID(ctx, planID)
	if err != nil {
		return nil, err
	}

	invoiceID := uuid.NewString()
	output, err := s.payment.CreatePaymentOrder(ctx, model.CreateOrderInput{
		InvoiceID: invoiceID,
		Money:     plan.Money,
		MethodID:  methodID,
	})
	if err != nil {
		slog.Error("Failed to create payment order", "err", err)
		return nil, err
	}

	invoice := &model.Invoice{
		ID:                   invoiceID,
		ProviderOrderID:      output.ProviderOrderID,
		UserID:               user.ID,
		PlanID:               plan.ID,
		ChatID:               chatID,
		PaymentProvider:      output.ProviderName,
		Money:                plan.Money,
		Status:               model.CreatedInvoiceStatus,
		CheckoutURL:          output.RedirectURL,
		CreatedAt:            time.Now().UTC(),
		ExpiresAt:            output.ExpiredAt,
		RenewsSubscriptionID: renewsSubscriptionID,
	}
	if err := s.storage.CreateInvoice(ctx, invoice); err != nil {
		return nil, err
	}

	return invoice, nil
}

func (s *ShopService) ActivateTrial(ctx context.Context, telegramID int64, username string, chatID int64) (*model.Subscription, error) {
	var sub *model.Subscription

	err := s.storage.WithTx(ctx, func(ctx context.Context) error {
		user, err := s.storage.UpsertUserByTelegramID(ctx, telegramID, username)
		if err != nil {
			return err
		}

		used, err := s.storage.HasUsedTrial(ctx, user.ID)
		if err != nil {
			return err
		}
		if used {
			return ErrTrialAlreadyUsed
		}

		plans, err := s.storage.GetAllTrialPlans(ctx)
		if err != nil {
			return err
		}
		if len(plans) == 0 {
			return ErrNoTrialPlanAvailable
		}
		plan := plans[0]

		now := time.Now().UTC()
		sub = &model.Subscription{
			ID:        uuid.NewString(),
			UserID:    user.ID,
			InvoiceID: nil,
			PlanID:    plan.ID,
			ChatID:    chatID,
			Status:    model.ActiveSubscriptionStatus,
			IsTrial:   true,
			StartAt:   now,
			EndAt:     now.Add(trialDuration),
		}
		if err := s.storage.CreateSubscription(ctx, sub); err != nil {
			return err
		}

		event := model.SubscriptionActivatedEvent{
			UserID:            sub.UserID,
			Region:            plan.Region,
			DriverType:        plan.DriverType,
			SubscribeDuration: trialDuration,
			SubscriptionID:    sub.ID,
			ChatID:            chatID,
		}
		return s.outbox.SaveOutboxEvent(ctx, model.EventTypeSubscriptionActivated, event)
	})
	if err != nil {
		return nil, err
	}

	return sub, nil
}
