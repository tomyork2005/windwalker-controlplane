package dto

import "errors"

type InitiatePaymentRequest struct {
	Username string `json:"username"`
	PlanID   string `json:"plan_id"`
	MethodID string `json:"method_id"`
}

func (req *InitiatePaymentRequest) Validate() error {
	if req.Username == "" {
		return errors.New("username is empty")
	}
	if req.PlanID == "" {
		return errors.New("plan_id is empty")
	}
	if req.MethodID == "" {
		return errors.New("payment_provider is empty")
	}

	return nil
}

type InitiatePaymentResponse struct {
	CheckoutURL string `json:"checkout_url"`
	ExpiresAt   int64  `json:"expires_at"`
}

type PlanDTO struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Region   string `json:"region"`
	Protocol string `json:"protocol"`
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}
