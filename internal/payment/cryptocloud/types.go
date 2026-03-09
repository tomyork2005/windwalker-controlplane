package cryptocloud

// https://docs.cryptocloud.plus/ru/api-reference-v2/create-invoice#id-200-ok-schet-sozdan
type invoiceCreateResponse struct {
	Status string              `json:"status"`
	Result invoiceCreateResult `json:"result"`
}

type invoiceCreateResult struct {
	UUID             string `json:"uuid"`
	Created          string `json:"created"`
	Address          string `json:"address"`
	ExpiryDate       string `json:"expiry_date"`
	SideCommission   string `json:"side_commission"`
	SideCommissionCC string `json:"side_commission_cc"`

	Amount       float64 `json:"amount"`
	AmountUSD    float64 `json:"amount_usd"`
	AmountInFiat float64 `json:"amount_in_fiat"`

	Fee           float64 `json:"fee"`
	FeeUSD        float64 `json:"fee_usd"`
	ServiceFee    float64 `json:"service_fee"`
	ServiceFeeUSD float64 `json:"service_fee_usd"`

	TypePayments string `json:"type_payments"`
	FiatCurrency string `json:"fiat_currency"`
	Status       string `json:"status"`

	IsEmailRequired bool   `json:"is_email_required"`
	Link            string `json:"link"`

	InvoiceID *string `json:"invoice_id"`

	Currency invoiceCurrency `json:"currency"`
	Project  invoiceProject  `json:"project"`

	TestMode bool `json:"test_mode"`
}

type invoiceProject struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	FailURL    string `json:"fail"`
	SuccessURL string `json:"success"`
	Logo       string `json:"logo"`
}

type invoiceCurrency struct {
	ID              int    `json:"id"`
	Code            string `json:"code"`
	FullCode        string `json:"fullcode"`
	Name            string `json:"name"`
	IsEmailRequired bool   `json:"is_email_required"`
	Stablecoin      bool   `json:"stablecoin"`
	IconBase        string `json:"icon_base"`
	IconNetwork     string `json:"icon_network"`
	IconQR          string `json:"icon_qr"`
	Order           int    `json:"order"`

	Network invoiceCurrencyNetwork `json:"network"`
}
type invoiceCurrencyNetwork struct {
	Code     string `json:"code"`
	ID       int    `json:"id"`
	Icon     string `json:"icon"`
	FullName string `json:"fullname"`
}

type postbackPayload struct {
	Status       string  `json:"status"`
	InvoiceID    string  `json:"invoice_id"`
	AmountCrypto float64 `json:"amount_crypto"`
	Currency     string  `json:"currency"`
	OrderID      string  `json:"order_id"`
	Token        string  `json:"token"`

	InvoiceInfo struct {
		UUID          string  `json:"uuid"`
		InvoiceStatus string  `json:"invoice_status"`
		AmountUSD     float64 `json:"amount_usd"`
		AmountPaidUSD float64 `json:"amount_paid_usd"`
		Created       string  `json:"created"`
		DateFinished  string  `json:"date_finished"`
		ExpiryDate    string  `json:"expiry_date"`
	} `json:"invoice_info"`
}
