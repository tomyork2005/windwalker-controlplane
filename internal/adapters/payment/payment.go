package payment

import (
	"context"
	"errors"
	"fmt"
	
	"control-plane/internal/domain"
)

var (
	ErrBadCreateOrderInput = errors.New("bad input")
	ErrBadSignature        = errors.New("bad token signature")
	ErrProviderNotFound    = errors.New("payment provider for method not found")
)

type Provider interface {
	Name() string
	Methods() []string
	CreatePaymentOrder(ctx context.Context, input domain.CreateOrderInput) (domain.CreateOrderOutput, error)
	VerifyCallback(input domain.CallbackInput) (domain.CallbackOutput, error)
}

type Payments struct {
	providersMap        map[string]Provider
	methodToProviderMap map[string]Provider
}

func NewPayments(providers ...Provider) *Payments {
	providersMap := make(map[string]Provider, len(providers))
	methodToProviderMap := make(map[string]Provider, len(providers))

	for _, pr := range providers {
		providersMap[pr.Name()] = pr
		for _, m := range pr.Methods() {
			methodToProviderMap[m] = pr // okay if the same method, we choose last
		}
	}

	return &Payments{providersMap, methodToProviderMap}
}

func (p *Payments) CreatePaymentOrder(ctx context.Context, input domain.CreateOrderInput) (domain.CreateOrderOutput, error) {
	var out domain.CreateOrderOutput

	err := input.Validate()
	if err != nil {
		return out, fmt.Errorf("%w: %s", ErrBadCreateOrderInput, err.Error())
	}

	pr, ok := p.methodToProviderMap[input.MethodID]
	if !ok {
		return out, ErrProviderNotFound
	}

	return pr.CreatePaymentOrder(ctx, input)
}

func (p *Payments) VerifyCallback(input domain.CallbackInput) (domain.CallbackOutput, error) {
	pr, ok := p.providersMap[input.ProviderName]
	if !ok {
		return domain.CallbackOutput{}, ErrProviderNotFound
	}

	return pr.VerifyCallback(input)
}
