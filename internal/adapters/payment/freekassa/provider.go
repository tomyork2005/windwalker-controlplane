package freekassa

import (
	"bytes"
	"context"
	"control-plane/internal/adapters/payment"
	"control-plane/internal/core"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"
)

type Provider struct {
	cfg   *Config
	httpC *http.Client
	nonce payment.Noncer
}

var _ core.PaymentProvider = (*Provider)(nil)

func NewProvider(cfg *Config, httpC *http.Client, nonce payment.Noncer) *Provider {
	if cfg.BaseAPI == "" {
		cfg.BaseAPI = "https://api.fk.life/v1"
	}
	if httpC == nil {
		httpC = &http.Client{Timeout: 10 * time.Second}
	}
	return &Provider{cfg: cfg, httpC: httpC, nonce: nonce}
}

func (p *Provider) StartPayment(ctx context.Context, in payment.StartPaymentInput) (payment.StartPaymentOutput, error) {
	err := in.Validate()
	if err != nil {
		return payment.StartPaymentOutput{}, err
	}

	// Check method id is available

	nonce := p.nonce.Next()
	params := map[string]string{
		"shopId": fmt.Sprintf("%d", p.cfg.ShopID),
		"nonce":  nonce,
	}
	signature := sign(params, p.cfg.APIKey)
	params["signature"] = signature

	body, _ := json.Marshal(params)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s%s%s%s", p.cfg.BaseAPI, "/currencies/", in.MethodID, "/status"), bytes.NewBuffer(body))
	if err != nil {
		return payment.StartPaymentOutput{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	// TODO parse success

	// Create order

	nonce = p.nonce.Next()
	params = map[string]string{
		"shopId":    fmt.Sprintf("%d", p.cfg.ShopID),
		"nonce":     nonce,
		"paymentId": in.InvoiceID,
		"i":         in.MethodID,
		"email":     p.cfg.DefaultEmail,
		"ip":        in.IP,
		"amount":    fmt.Sprintf("%d", in.Money.Amount),
		"currency":  string(in.Money.Curr),
	}

	signature = sign(params, p.cfg.APIKey)
	params["signature"] = signature

	body, _ = json.Marshal(params)
	req, err = http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.BaseAPI+"/orders/create", bytes.NewBuffer(body))
	if err != nil {
		return payment.StartPaymentOutput{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpC.Do(req)
	if err != nil {
		return payment.StartPaymentOutput{}, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return payment.StartPaymentOutput{Raw: raw}, fmt.Errorf("fk create order: status=%d body=%s", resp.StatusCode, string(raw))
	}

	var out createOrderResponse
	err = json.Unmarshal(raw, &out)
	if err != nil {
		return payment.StartPaymentOutput{Raw: raw}, fmt.Errorf("fk create order, cant unmarshal: err=%w", err)
	}
	if out.Location == "" {
		return payment.StartPaymentOutput{Raw: raw}, fmt.Errorf("fk create order: empty location")
	}

	return payment.StartPaymentOutput{
		RedirectURL:     out.Location,
		ProviderOrderID: out.OrderID,
		Raw:             raw,
	}, nil
}

func (p *Provider) VerifyCallback(r *http.Request) (payment.Callback, error) {
	if err := r.ParseForm(); err != nil {
		return payment.Callback{}, err
	}

	ip, ok := extractAndVerifyIP(r, p.cfg.IPWhitelist)
	if !ok {
		return payment.Callback{}, payment.ErrIPNotAllowed
	}

	callback := payment.Callback{
		MerchantID: r.FormValue("MERCHANT_ID"),
		Amount:     r.FormValue("AMOUNT"),
		OrderID:    r.FormValue("MERCHANT_ORDER_ID"),
		FKOrderID:  r.FormValue("intid"),
		CurrencyID: r.FormValue("CUR_ID"),
		Email:      r.FormValue("P_EMAIL"),
		Phone:      r.FormValue("P_PHONE"),
		Signature:  r.FormValue("SIGN"),
		RemoteIP:   ip,
		All:        map[string]string{},
		US:         map[string]string{},
	}
	for k := range r.Form {
		v := r.Form.Get(k)
		callback.All[k] = v
		if strings.HasPrefix(k, "us_") {
			callback.US[strings.TrimPrefix(k, "us_")] = v
		}
	}

	// md5(MERCHANT_ID:AMOUNT:SECRET2:MERCHANT_ORDER_ID) - fk docs
	exp := md5hex(fmt.Sprintf("%s:%s:%s:%s", callback.MerchantID, callback.Amount, p.cfg.Secret2, callback.OrderID))

	if !hmac.Equal([]byte(exp), []byte(strings.ToLower(callback.Signature))) &&
		!hmac.Equal([]byte(exp), []byte(callback.Signature)) {
		return payment.Callback{}, payment.ErrBadSignature
	}

	return callback, nil
}

// helpers

func sign(params map[string]string, key string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		if k == "signature" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	vals := make([]string, 0, len(keys))
	for _, k := range keys {
		vals = append(vals, params[k])
	}

	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(strings.Join(vals, "|")))
	return hex.EncodeToString(mac.Sum(nil))
}

func extractAndVerifyIP(r *http.Request, trusted []string) (string, bool) {
	host, _, _ := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if host == "" {
		host = strings.TrimSpace(r.RemoteAddr) // fallback
	}
	for _, ip := range trusted {
		if host == ip {
			return host, true
		}
	}
	return host, false
}

func md5hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

type createOrderResponse struct {
	Type           string `json:"type"`
	OrderID        int64  `json:"orderId"`
	Location       string `json:"location"`
	OrderHash      string `json:"orderHash"`
	RecurrentOrder *struct {
		ID        string `json:"id"`
		PayDateAt string `json:"pay_date_at"`
	} `json:"recurrent_order,omitempty"`
}
