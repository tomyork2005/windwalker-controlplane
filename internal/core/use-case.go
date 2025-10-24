package core

import (
	"context"
	"control-plane/internal/adapters/agent"
	"control-plane/internal/adapters/payment"
	"control-plane/internal/domain"
	"github.com/google/uuid"
	"golang.org/x/sys/windows/registry"
	"net/http"
)

type PaymentProvider interface {
	StartPayment(ctx context.Context, input payment.StartPaymentInput) (payment.StartPaymentOutput, error)
	VerifyCallback(r *http.Request) (payment.Callback, error)
}

type AgentService interface {
	UpsertUser(ctx context.Context, input agent.UserUpsertInput) error
	RemoveUser(ctx context.Context, input agent.RemoveUserInput) error
}

type storage interface {
	UpsertUser(ctx context.Context, uuid string, username string) (*domain.User, error)
	GetPlanByID(ctx context.Context, planID string) (*domain.Plan, error)
}

type Service struct {
	storage storage
	agent   AgentService
	payment PaymentProvider
}

func (s *Service) ListPlans() {}

func (s *Service) StartPayment(ctx context.Context, planID string, username string, methodID string) (domain.Invoice, error) {

	user, err := s.storage.UpsertUser(ctx, uuid.NewString(), username)
	if err != nil {
		return domain.Invoice{}, err
	}

	plan, err := s.storage.GetPlanByID(ctx, planID)
	if err != nil {
		return domain.Invoice{}, err
	}

	output, err := s.payment.StartPayment(ctx, payment.StartPaymentInput{
		InvoiceID: uuid.New().String(),
		Money:     plan.Money,
		MethodID:  methodID,
		IP:        "0.0.0.0", // idk how to get ip from telegram
	})
	if err != nil {
		return domain.Invoice{}, err
	}

	invoice, err := s.storage.CreateInvoice()

	return invoice, err
}
