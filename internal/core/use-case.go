package core

import (
	"context"
	"control-plane/internal/adapters/agent"
	"control-plane/internal/adapters/payment"
	"control-plane/internal/domain"
	"github.com/google/uuid"
	"time"
)

type paymentService interface {
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

	CreateInvoice(ctx context.Context, invoice domain.Invoice) error
	UpdateInvoice(ctx context.Context, invoice domain.Invoice) error
}

type Service struct {
	storage storage
	agent   agentClient
	payment paymentService
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

func (s *Service) CreateInvoice(ctx context.Context, planID string, username string, methodID string) (domain.Invoice, error) {
	user, err := s.storage.UpsertUser(ctx, uuid.NewString(), username)
	if err != nil {
		return domain.Invoice{}, err
	}

	plan, err := s.storage.GetPlanByID(ctx, planID)
	if err != nil {
		return domain.Invoice{}, err
	}

	output, err := s.payment.CreatePaymentOrder(ctx, domain.CreateOrderInput{
		InvoiceID: uuid.New().String(),
		Money:     plan.Money,
		MethodID:  methodID,
	})
	if err != nil {
		return domain.Invoice{}, err
	}

	invoice := domain.Invoice{
		ID:              uuid.New().String(),
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
		return domain.Invoice{}, err
	}

	return invoice, err
}

func (s *Service) ProcessPaymentCallback(ctx context.Context, req domain.CallbackInput) error {
	callback, err := s.payment.VerifyCallback(req)
	if err != nil {
		// todo route errors
		return err
	}

	invoice :=

}
