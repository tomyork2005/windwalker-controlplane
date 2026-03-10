package service

import (
	"context"
	"control-plane/internal/model"
	"github.com/google/uuid"
	"log/slog"
	"time"
)

type ShopPayment interface {
	CreatePaymentOrder(ctx context.Context, input model.CreateOrderInput) (model.CreateOrderOutput, error)
	VerifyCallback(input model.CallbackInput) (model.CallbackOutput, error)
}

type ShopStorage interface {
	UpsertUser(ctx context.Context, uuid string, username string) (*model.User, error)
	GetAllPlans(ctx context.Context) ([]*model.Plan, error)
	GetPlanByID(ctx context.Context, planID string) (*model.Plan, error)
	GetAllPaymentMethods(ctx context.Context) ([]*model.PaymentMethod, error)
	CreateInvoice(ctx context.Context, invoice *model.Invoice) error

	WithTx(ctx context.Context, fn func(ctx context.Context) error) error
}

type ShopService struct {
	payment ShopPayment
	storage ShopStorage
}

func NewShopService(payment ShopPayment, storage ShopStorage) *ShopService {
	return &ShopService{
		storage: storage,
		payment: payment,
	}
}

func (s *ShopService) ListPaymentMethods(ctx context.Context) ([]*model.PaymentMethod, error) {
	methods, err := s.storage.GetAllPaymentMethods(ctx)
	if err != nil {
		return nil, err
	}

	return methods, nil
}

func (s *ShopService) ListPlans(ctx context.Context) ([]*model.Plan, error) {
	plans, err := s.storage.GetAllPlans(ctx)
	if err != nil {
		return nil, err
	}

	return plans, nil
}

func (s *ShopService) CreateInvoice(ctx context.Context, planID string, username string, methodID string, chatID int64) (*model.Invoice, error) {
	user, err := s.storage.UpsertUser(ctx, uuid.NewString(), username)
	if err != nil {
		return nil, err
	}

	slog.Info("CreateInvoice user:", "planID:", planID, "methodID:", methodID, "chat_id:", chatID)

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
		ID:              invoiceID,
		UserID:          user.ID,
		PlanID:          plan.ID,
		ChatID:          chatID,
		PaymentProvider: output.ProviderName,
		Money:           plan.Money,
		Status:          model.CreatedInvoiceStatus,
		CheckoutURL:     output.RedirectURL,
		CreatedAt:       time.Now().UTC(),
		ExpiresAt:       output.ExpiredAt,
	}
	err = s.storage.CreateInvoice(ctx, invoice)
	if err != nil {
		return nil, err
	}

	return invoice, err
}
