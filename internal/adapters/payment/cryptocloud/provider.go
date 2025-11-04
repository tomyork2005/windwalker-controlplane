package cryptocloud

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"control-plane/internal/adapters/payment"
	"control-plane/internal/domain"

	"github.com/golang-jwt/jwt/v5"
)

const providerName = "CryptoCloud"

type Provider struct {
	cfg   Config
	httpC *http.Client
}

func NewProvider(cfg Config, client *http.Client) *Provider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.cryptocloud.plus/v2"
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	return &Provider{
		cfg:   cfg,
		httpC: client,
	}
}

func (p *Provider) Name() string {
	return providerName
}

func (p *Provider) Methods() []string {
	return p.cfg.Methods
}

func (p *Provider) CreatePaymentOrder(ctx context.Context, input domain.CreateOrderInput) (domain.CreateOrderOutput, error) {
	var out domain.CreateOrderOutput

	if input.Money.Curr != domain.USDCurrency {
		return out, errors.New("for crypto payment method currency must be USD")
	}

	amount := float64(input.Money.Amount) / 100 // cents 100 -> $1.00
	params := map[string]any{
		"shop_id":  p.cfg.ShopID,
		"amount":   amount,
		"currency": string(domain.USDCurrency),
		"order_id": input.InvoiceID,
		"email":    p.cfg.DefaultEmail,
		"add_fields": map[string]any{
			"time_to_pay": map[string]int{
				"hours":   p.cfg.OrderTTL,
				"minutes": 0,
			},
		},
	}
	body, err := json.Marshal(params)
	if err != nil {
		return out, fmt.Errorf("cryptocloud: marshalling request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s%s", p.cfg.BaseURL, "/invoice/create"), bytes.NewBuffer(body))
	if err != nil {
		return out, fmt.Errorf("cryptocloud: new request: %w", err)
	}
	req.Header.Add("Authorization", fmt.Sprintf("Token %s", p.cfg.ApiKey))
	req.Header.Add("Content-Type", "application/json")

	resp, err := p.httpC.Do(req)
	if err != nil {
		return out, fmt.Errorf("cryptocloud: do request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return out, fmt.Errorf("cryptocloud: read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("cryptocloud: unexpected status %d: %s", resp.StatusCode, string(raw))
	}

	var response invoiceCreateResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return out, fmt.Errorf("cryptocloud: decode json: %w", err)
	}

	if response.Status != "success" {
		return out, fmt.Errorf("cryptocloud: invoice create failed: %s", response.Status)
	}

	expiredAt, err := parseCloudCryptoTime(response.Result.ExpiryDate)
	if err != nil {
		return out, fmt.Errorf("cryptocloud: parse expiry date: %w", err)
	}

	out = domain.CreateOrderOutput{
		ProviderName:    providerName,
		RedirectURL:     response.Result.Link,
		ProviderOrderID: response.Result.UUID,
		ExpiredAt:       expiredAt,
		Raw:             raw,
	}

	return out, nil
}

func (p *Provider) VerifyCallback(input domain.CallbackInput) (domain.CallbackOutput, error) {
	var out domain.CallbackOutput

	var payload postbackPayload
	if err := json.Unmarshal(input.Body, &payload); err != nil {
		return out, fmt.Errorf("cryptocloud: decode json: %w", err)
	}

	if err := p.verifyJwtToken(payload.Token); err != nil {
		return out, fmt.Errorf("cryptocloud: verify token: %w", err)
	}

	if payload.Status != "success" {
		return out, fmt.Errorf("cryptocloud: verify failed: %s", payload.Status)
	}

	paidAt, err := parseCloudCryptoTime(payload.InvoiceInfo.DateFinished)
	if err != nil {
		return out, fmt.Errorf("cryptocloud: parse date_finished %q: %w",
			payload.InvoiceInfo.DateFinished, err)
	}

	out = domain.CallbackOutput{
		ProviderName:   p.Name(),
		Status:         mapStatus(payload.InvoiceInfo.InvoiceStatus),
		InvoiceID:      payload.OrderID,   // inbound id
		PaymentOrderID: payload.InvoiceID, // outbound id
		PaidAt:         paidAt,
	}

	return out, nil
}

func (p *Provider) verifyJwtToken(tokenString string) error {
	if tokenString == "" {
		return payment.ErrBadSignature
	}

	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
		method, ok := t.Method.(*jwt.SigningMethodHMAC)
		if !ok || method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("cryptocloud: unexpected jwt signing method: %v", t.Header["alg"])
		}

		return []byte(p.cfg.ApiSecret), nil
	})
	if err != nil {
		return fmt.Errorf("%w: %v", payment.ErrBadSignature, err)
	}

	if !token.Valid {
		return payment.ErrBadSignature
	}

	return nil
}

func parseCloudCryptoTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	const layout = "2006-01-02 15:04:05.999999"
	return time.ParseInLocation(layout, s, time.UTC)
}

func mapStatus(status string) domain.InvoiceStatus {
	switch status {
	case "success":
		return domain.SuccessInvoiceStatus
	case "failed":
		return domain.CanceledInvoiceStatus
	default:
		return domain.UnknownInvoiceStatus
	}
}
