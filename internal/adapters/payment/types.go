package payment

import (
	"control-plane/internal/domain"
	"errors"
)

type StartPaymentInput struct {
	InvoiceID string
	Money     domain.Money
	MethodID  string
	IP        string
}

func (i *StartPaymentInput) Validate() error {
	if i.InvoiceID == "" {
		return errors.New("validation error - missing InvoiceID")
	}
	if i.MethodID == "" {
		return errors.New("validation error - missing MethodID")
	}
	if i.IP == "" {
		return errors.New("validation error - missing IP")
	}

	return nil
}

type StartPaymentOutput struct {
	RedirectURL     string
	ProviderOrderID int64
	Raw             []byte
}

type Callback struct {
	MerchantID string
	Amount     string
	FKOrderID  string
	OrderID    string
	Email      string
	CurrencyID string
	Phone      string
	Signature  string
	US         map[string]string
	RemoteIP   string
	All        map[string]string
}
