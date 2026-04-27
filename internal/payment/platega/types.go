package platega

type createRequest struct {
	PaymentMethod  int            `json:"paymentMethod"`
	PaymentDetails paymentDetails `json:"paymentDetails"`
	Description    string         `json:"description,omitempty"`
	Return         string         `json:"return,omitempty"`
	FailedURL      string         `json:"failedUrl,omitempty"`
	Payload        string         `json:"payload,omitempty"`
}

type paymentDetails struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

type createResponse struct {
	TransactionID string `json:"transactionId"`
	Redirect      string `json:"redirect"`
	Status        string `json:"status"`
	ExpiresIn     string `json:"expiresIn"`
}

type callbackPayload struct {
	ID            string  `json:"id"`
	Amount        float64 `json:"amount"`
	Currency      string  `json:"currency"`
	Status        string  `json:"status"`
	PaymentMethod int     `json:"paymentMethod"`
}
