package platega

import (
	"bytes"
	"cmp"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"control-plane/internal/config"
	"control-plane/internal/model"
	"control-plane/internal/payment"
)

const providerName = "Platega"

const (
	headerMerchantID = "X-MerchantId"
	headerSecret     = "X-Secret"
)

const fallbackTTL = time.Hour

var methodToCode = map[string]int{
	"sbp":            2,
	"crypto_platega": 13,
}

type Provider struct {
	cfg   config.PlategaConfig
	httpC *http.Client
}

func NewProvider(cfg config.PlategaConfig, client *http.Client) *Provider {
	cfg.BaseURL = cmp.Or(cfg.BaseURL, "https://app.platega.io")
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Provider{cfg: cfg, httpC: client}
}

func (p *Provider) Name() string {
	return providerName
}

func (p *Provider) Methods() []string {
	return p.cfg.Methods
}

func (p *Provider) CreatePaymentOrder(ctx context.Context, input model.CreateOrderInput) (model.CreateOrderOutput, error) {
	var out model.CreateOrderOutput

	code, ok := methodToCode[input.MethodID]
	if !ok {
		return out, fmt.Errorf("%w: unknown platega method %q", payment.ErrBadCreateOrderInput, input.MethodID)
	}

	if input.Money.Curr != model.RUBCurrency {
		return out, fmt.Errorf("%w: platega supports only RUB, got %q", payment.ErrBadCreateOrderInput, input.Money.Curr)
	}

	body, err := json.Marshal(createRequest{
		PaymentMethod: code,
		PaymentDetails: paymentDetails{
			Amount:   float64(input.Money.Amount),
			Currency: string(input.Money.Curr),
		},
		Description: fmt.Sprintf("Invoice %s", input.InvoiceID),
		Return:      p.cfg.ReturnURL,
		FailedURL:   p.cfg.FailedURL,
		Payload:     input.InvoiceID,
	})
	if err != nil {
		return out, fmt.Errorf("platega: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.BaseURL+"/transaction/process", bytes.NewBuffer(body))
	if err != nil {
		return out, fmt.Errorf("platega: new request: %w", err)
	}
	req.Header.Set(headerMerchantID, p.cfg.MerchantID)
	req.Header.Set(headerSecret, p.cfg.Secret)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpC.Do(req)
	if err != nil {
		return out, fmt.Errorf("platega: do request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return out, fmt.Errorf("platega: read body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return out, fmt.Errorf("platega: unexpected status %d: %s", resp.StatusCode, string(raw))
	}

	var response createResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return out, fmt.Errorf("platega: decode response: %w", err)
	}

	if response.TransactionID == "" || response.Redirect == "" {
		return out, fmt.Errorf("platega: empty transactionId or redirect in response: %s", string(raw))
	}

	ttl, err := parseExpiresIn(response.ExpiresIn)
	if err != nil {
		slog.Warn("platega: cannot parse expiresIn, using fallback", "value", response.ExpiresIn, "err", err)
		ttl = fallbackTTL
	}

	out = model.CreateOrderOutput{
		ProviderName:    providerName,
		ProviderOrderID: response.TransactionID,
		RedirectURL:     response.Redirect,
		ExpiredAt:       time.Now().UTC().Add(ttl),
		Raw:             raw,
	}

	return out, nil
}

func (p *Provider) VerifyCallback(input model.CallbackInput) (model.CallbackOutput, error) {
	var out model.CallbackOutput

	gotMID := input.Headers[headerMerchantID]
	gotSec := input.Headers[headerSecret]
	if subtle.ConstantTimeCompare([]byte(gotMID), []byte(p.cfg.MerchantID)) != 1 ||
		subtle.ConstantTimeCompare([]byte(gotSec), []byte(p.cfg.Secret)) != 1 {
		return out, payment.ErrBadSignature
	}

	var pl callbackPayload
	if err := json.Unmarshal(input.Body, &pl); err != nil {
		return out, fmt.Errorf("platega: decode callback: %w", err)
	}

	if pl.ID == "" {
		return out, fmt.Errorf("platega: callback has empty id")
	}

	mapped := mapStatus(pl.Status)
	slog.Info("platega callback decoded",
		"transaction_id", pl.ID,
		"provider_status", pl.Status,
		"mapped_status", mapped,
		"amount", pl.Amount,
		"currency", pl.Currency,
	)

	out = model.CallbackOutput{
		ProviderName:   providerName,
		Status:         mapped,
		PaymentOrderID: pl.ID,
		PaidAt:         time.Now().UTC(),
	}

	return out, nil
}

func mapStatus(s string) model.InvoiceStatus {
	switch s {
	case "CONFIRMED":
		return model.SuccessInvoiceStatus
	case "CANCELED", "CHARGEBACKED":
		return model.CanceledInvoiceStatus
	default:
		return model.UnknownInvoiceStatus
	}
}

func parseExpiresIn(s string) (time.Duration, error) {
	if s == "" {
		return 0, fmt.Errorf("empty expiresIn")
	}
	var h, m, sec int
	if _, err := fmt.Sscanf(s, "%d:%d:%d", &h, &m, &sec); err != nil {
		return 0, err
	}
	return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute + time.Duration(sec)*time.Second, nil
}
