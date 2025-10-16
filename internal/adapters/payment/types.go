package payment

import "errors"

var (
	ErrIPNotAllowed = errors.New("IP not allowed ")
	ErrBadSignature = errors.New("Bad signature ")
)

type Currency string

const (
	RUB Currency = "RUB"
	USD Currency = "USD"
	EUR Currency = "EUR"
)

type StartPaymentInput struct {
	InvoiceID string
	Amount    string
	Currency  Currency
	Email     string
	MethodID  string
	IP        string
}

func (i *StartPaymentInput) Validate() error {
	if i.InvoiceID == "" {
		return errors.New("validation error - missing InvoiceID")
	}
	if i.Amount == "" {
		return errors.New("validation error - missing Amount")
	}
	if i.Currency == "" {
		return errors.New("validation error - missing Currency")
	}
	if i.Email == "" {
		return errors.New("validation error - missing Email")
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
