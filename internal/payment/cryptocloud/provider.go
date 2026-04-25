package cryptocloud

import (
	"bytes"
	"context"
	"control-plane/internal/config"
	"control-plane/internal/payment"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"control-plane/internal/model"

	"github.com/golang-jwt/jwt/v5"
)

const providerName = "CryptoCloud"

type Provider struct {
	cfg   config.CryptoCloudConfig
	httpC *http.Client
}

func NewProvider(cfg config.CryptoCloudConfig, client *http.Client) *Provider {
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

func (p *Provider) CreatePaymentOrder(ctx context.Context, input model.CreateOrderInput) (model.CreateOrderOutput, error) {
	var out model.CreateOrderOutput

	amountMinor, err := toMinorUnits(input.Money)
	if err != nil {
		return out, err
	}

	params := map[string]any{
		"shop_id":  p.cfg.ShopID,
		"amount":   amountMinor,
		"currency": string(input.Money.Curr),
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
		slog.Error("cryptocloud: unmarshalling response: %w", err, "response", string(raw))
		return out, fmt.Errorf("cryptocloud: resp decode json: %w", err)
	}

	if response.Status != "success" {
		return out, fmt.Errorf("cryptocloud: invoice create failed: %s", response.Status)
	}

	expiredAt, err := parseCloudCryptoTime(response.Result.ExpiryDate)
	if err != nil {
		return out, fmt.Errorf("cryptocloud: parse expiry date: %w", err)
	}

	out = model.CreateOrderOutput{
		ProviderName:    providerName,
		RedirectURL:     response.Result.Link,
		ProviderOrderID: response.Result.UUID,
		ExpiredAt:       expiredAt,
		Raw:             raw,
	}

	return out, nil
}

func (p *Provider) VerifyCallback(input model.CallbackInput) (model.CallbackOutput, error) {
	var out model.CallbackOutput

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

	out = model.CallbackOutput{
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

func mapStatus(status string) model.InvoiceStatus {
	switch status {
	case "success":
		return model.SuccessInvoiceStatus
	case "failed":
		return model.CanceledInvoiceStatus
	default:
		return model.UnknownInvoiceStatus
	}
}

// toMinorUnits переводит сумму из основных единиц (рубли/доллары)
// в копейки/центы — CryptoCloud принимает amount в минимальных единицах.
func toMinorUnits(m model.Money) (int64, error) {
	switch m.Curr {
	case model.RUBCurrency, model.USDCurrency:
		return m.Amount * 100, nil
	default:
		return 0, fmt.Errorf("cryptocloud: unsupported currency %q", m.Curr)
	}
}
