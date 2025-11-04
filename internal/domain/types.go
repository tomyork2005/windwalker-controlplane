package domain

import (
	"errors"
	"time"
)

type Currency string

type Money struct {
	Amount int64
	Curr   Currency
}

const (
	RUBCurrency Currency = "RUB"
	USDCurrency Currency = "USD"
)

var supportedCurrencies = map[Currency]struct{}{
	RUBCurrency: {},
	USDCurrency: {},
}

var (
	ErrNegativeAmount  = errors.New("amount must be non-negative")
	ErrInvalidCurrency = errors.New("invalid currency")
)

type InvoiceStatus string

const (
	CreatedInvoiceStatus  InvoiceStatus = "created"
	CanceledInvoiceStatus InvoiceStatus = "canceled"
	SuccessInvoiceStatus  InvoiceStatus = "success"
	UnknownInvoiceStatus  InvoiceStatus = "unknown"
)

type User struct {
	ID       string
	Username string
}
type Plan struct {
	ID       string
	Name     string
	Region   string
	Protocol string
	Money    Money
	Archived bool
}

type Invoice struct {
	ID              string
	UserID          string
	PlanID          string
	PaymentProvider string
	Money           Money
	Status          InvoiceStatus
	CheckoutURL     string
	CreatedAt       time.Time
	ExpiresAt       time.Time
	PaidAt          time.Time
}

type PaymentMethod struct {
	ID   string
	Name string
}

type CreateOrderInput struct {
	InvoiceID string
	Money     Money
	MethodID  string
}

type CallbackInput struct {
	ProviderName string
	Body         []byte
}

type CreateOrderOutput struct {
	ProviderName    string
	ProviderOrderID string
	RedirectURL     string
	ExpiredAt       time.Time
	Raw             []byte
}

type CallbackOutput struct {
	ProviderName   string
	Status         InvoiceStatus
	InvoiceID      string
	PaymentOrderID string
	PaidAt         time.Time
}

type Agent struct {
	ID          string
	InstanceID  string
	Region      string
	Version     string
	DriverTypes []string
}

type Subscription struct {
	ID         string
	UserID     string
	PlanID     string
	Status     string
	StartAt    time.Time
	EndAt      time.Time
	CanceledAt time.Time
}

type AccessLink struct {
	ID             string
	UserID         string
	AgentID        string
	SubscriptionID string
	RequestID      string
	Protocol       string
	Link           string
	ExpiresAt      time.Time
}

func NewMoney(amount int64, currency string) (Money, error) {
	if amount < 0 {
		return Money{}, ErrNegativeAmount
	}

	_, ok := supportedCurrencies[Currency(currency)]
	if !ok {
		return Money{}, ErrInvalidCurrency
	}

	return Money{Amount: amount, Curr: Currency(currency)}, nil
}

func (i *CreateOrderInput) Validate() error {
	if i.InvoiceID == "" {
		return errors.New("validation error - missing InvoiceID")
	}
	if i.MethodID == "" {
		return errors.New("validation error - missing MethodID")
	}

	return nil
}
