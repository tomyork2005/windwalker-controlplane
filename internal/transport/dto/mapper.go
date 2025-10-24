package dto

import "control-plane/internal/domain"

func FromDomainPlanToDTO(plan domain.Plan) PlanDTO {
	return PlanDTO{
		ID:       plan.ID,
		Name:     plan.Name,
		Region:   plan.Region,
		Protocol: plan.Protocol,
		Amount:   plan.Money.Amount,
		Currency: string(plan.Money.Curr),
	}
}

func FromDomainInvoiceToInitiatePaymentResponse(invoice domain.Invoice) InitiatePaymentResponse {
	return InitiatePaymentResponse{
		CheckoutURL: invoice.CheckoutURL,
		ExpiresAt:   invoice.ExpiresAt.Unix(),
	}
}
