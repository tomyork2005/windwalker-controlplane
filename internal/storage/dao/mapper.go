package dao

import (
	"control-plane/internal/domain"
	"time"
)

const layout = "2006-01-02 15:04:05.999999"

func FromDaoPlanToDomainPlan(plan PlanDAO) (*domain.Plan, error) {
	money, err := domain.NewMoney(plan.Amount, plan.Currency)
	if err != nil {
		return nil, err
	}

	return &domain.Plan{
		ID:           plan.ID,
		Name:         plan.Name,
		Region:       plan.Region,
		Protocol:     plan.Protocol,
		Money:        money,
		DurationDays: plan.DurationDays,
		Archived:     plan.Archived,
	}, err
}

func FromDaoInvoiceToDomainInvoice(invoice InvoiceDAO) (*domain.Invoice, error) {
	money, err := domain.NewMoney(invoice.Amount, invoice.Currency)
	if err != nil {
		return nil, err
	}

	created, err := time.ParseInLocation(layout, invoice.CreatedAt, time.UTC)
	if err != nil {
		return nil, err
	}
	expire, err := time.ParseInLocation(layout, invoice.ExpiresAt, time.UTC)
	if err != nil {
		return nil, err
	}
	paid, err := time.ParseInLocation(layout, invoice.PaidAt, time.UTC)
	if err != nil {
		return nil, err
	}

	return &domain.Invoice{
		ID:              invoice.ID,
		UserID:          invoice.UserID,
		PlanID:          invoice.PlanID,
		PaymentProvider: invoice.PaymentProvider,
		Money:           money,
		CheckoutURL:     invoice.CheckoutURL,
		CreatedAt:       created,
		ExpiresAt:       expire,
		PaidAt:          paid,
	}, nil
}
