package payment

import (
	"context"
	"errors"
	"net/http"
)

var (
	ErrIPNotAllowed     = errors.New("IP not allowed ")
	ErrBadSignature     = errors.New("Bad signature ")
	ErrProviderNotFound = errors.New("provider who`s supported this method not found")
)

type Payments struct {
	providers map[string]provider
}

type provider interface {
	Name() string
	StartPayment(ctx context.Context, input StartPaymentInput) (StartPaymentOutput, error)
	VerifyCallback(r *http.Request) (Callback, error)
}

func (p *Payments) StartPayment(ctx context.Context, input StartPaymentInput) (StartPaymentOutput, error) {
	pr, ok := p.providers[input.MethodID]
	if !ok {
		return StartPaymentOutput{}, ErrProviderNotFound
	}

	return pr.StartPayment(ctx, input)
}

func (p *Payments) VerifyCallback(r *http.Request) (Callback, error) {
	// TODO
	return Callback{}, nil
}
