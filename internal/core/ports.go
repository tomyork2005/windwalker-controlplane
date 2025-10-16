package core

import (
	"context"
	"control-plane/internal/adapters/payment"
	"net/http"
)

type PaymentProvider interface {
	StartPayment(ctx context.Context, in payment.StartPaymentInput) (payment.StartPaymentOutput, error)
	VerifyCallback(r *http.Request) (payment.Callback, error)
}
