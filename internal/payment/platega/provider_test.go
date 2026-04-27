package platega

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"control-plane/internal/config"
	"control-plane/internal/model"
	"control-plane/internal/payment"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testMerchantID = "merchant-uuid"
	testSecret     = "api-secret"
)

func newTestProvider(t *testing.T, baseURL string) *Provider {
	t.Helper()
	return NewProvider(config.PlategaConfig{
		MerchantID: testMerchantID,
		Secret:     testSecret,
		BaseURL:    baseURL,
		Methods:    []string{"sbp", "crypto_platega"},
		ReturnURL:  "https://ret.example",
		FailedURL:  "https://fail.example",
	}, nil)
}

func TestProvider_CreatePaymentOrder(t *testing.T) {
	t.Parallel()

	const invoiceID = "inv-123"

	type serverResp struct {
		status int
		body   string
	}

	tests := []struct {
		name           string
		methodID       string
		money          model.Money
		serverResp     *serverResp
		serverAssert   func(t *testing.T, r *http.Request, body []byte)
		wantErr        error
		wantErrContain string
		assertOut      func(t *testing.T, out model.CreateOrderOutput)
	}{
		{
			name:     "sbp_ok",
			methodID: "sbp",
			money:    model.Money{Amount: 200, Curr: model.RUBCurrency},
			serverResp: &serverResp{
				status: http.StatusOK,
				body:   `{"transactionId":"tx-1","redirect":"https://pay/x","status":"PENDING","expiresIn":"00:15:00"}`,
			},
			serverAssert: func(t *testing.T, r *http.Request, body []byte) {
				assert.Equal(t, "POST", r.Method)
				assert.Equal(t, "/transaction/process", r.URL.Path)
				assert.Equal(t, testMerchantID, r.Header.Get("X-MerchantId"))
				assert.Equal(t, testSecret, r.Header.Get("X-Secret"))
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

				var req createRequest
				require.NoError(t, json.Unmarshal(body, &req))
				assert.Equal(t, 2, req.PaymentMethod)
				assert.Equal(t, 200.0, req.PaymentDetails.Amount)
				assert.Equal(t, "RUB", req.PaymentDetails.Currency)
				assert.Equal(t, "Invoice "+invoiceID, req.Description)
				assert.Equal(t, invoiceID, req.Payload)
				assert.Equal(t, "https://ret.example", req.Return)
				assert.Equal(t, "https://fail.example", req.FailedURL)
			},
			assertOut: func(t *testing.T, out model.CreateOrderOutput) {
				assert.Equal(t, "Platega", out.ProviderName)
				assert.Equal(t, "tx-1", out.ProviderOrderID)
				assert.Equal(t, "https://pay/x", out.RedirectURL)
				assert.WithinDuration(t, time.Now().UTC().Add(15*time.Minute), out.ExpiredAt, 5*time.Second)
			},
		},
		{
			name:     "crypto_platega_ok",
			methodID: "crypto_platega",
			money:    model.Money{Amount: 500, Curr: model.RUBCurrency},
			serverResp: &serverResp{
				status: http.StatusOK,
				body:   `{"transactionId":"tx-2","redirect":"https://pay/y","status":"PENDING","expiresIn":"01:00:00"}`,
			},
			serverAssert: func(t *testing.T, r *http.Request, body []byte) {
				var req createRequest
				require.NoError(t, json.Unmarshal(body, &req))
				assert.Equal(t, 13, req.PaymentMethod)
				assert.Equal(t, 500.0, req.PaymentDetails.Amount)
			},
			assertOut: func(t *testing.T, out model.CreateOrderOutput) {
				assert.Equal(t, "tx-2", out.ProviderOrderID)
				assert.WithinDuration(t, time.Now().UTC().Add(time.Hour), out.ExpiredAt, 5*time.Second)
			},
		},
		{
			name:     "unknown_method",
			methodID: "paypal",
			money:    model.Money{Amount: 200, Curr: model.RUBCurrency},
			wantErr:  payment.ErrBadCreateOrderInput,
		},
		{
			name:     "wrong_currency",
			methodID: "sbp",
			money:    model.Money{Amount: 200, Curr: model.USDCurrency},
			wantErr:  payment.ErrBadCreateOrderInput,
		},
		{
			name:     "non_2xx",
			methodID: "sbp",
			money:    model.Money{Amount: 200, Curr: model.RUBCurrency},
			serverResp: &serverResp{
				status: http.StatusInternalServerError,
				body:   `oops`,
			},
			wantErrContain: "500",
		},
		{
			name:     "empty_transaction_id",
			methodID: "sbp",
			money:    model.Money{Amount: 200, Curr: model.RUBCurrency},
			serverResp: &serverResp{
				status: http.StatusOK,
				body:   `{"transactionId":"","redirect":"https://pay/x","status":"PENDING"}`,
			},
			wantErrContain: "empty transactionId",
		},
		{
			name:     "bad_expires_in_uses_fallback",
			methodID: "sbp",
			money:    model.Money{Amount: 200, Curr: model.RUBCurrency},
			serverResp: &serverResp{
				status: http.StatusOK,
				body:   `{"transactionId":"tx-3","redirect":"https://pay/z","status":"PENDING","expiresIn":"garbage"}`,
			},
			assertOut: func(t *testing.T, out model.CreateOrderOutput) {
				assert.Equal(t, "tx-3", out.ProviderOrderID)
				assert.WithinDuration(t, time.Now().UTC().Add(fallbackTTL), out.ExpiredAt, 5*time.Second)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var srv *httptest.Server
			if tc.serverResp != nil {
				srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body := make([]byte, r.ContentLength)
					if r.ContentLength > 0 {
						_, _ = r.Body.Read(body)
					}
					if tc.serverAssert != nil {
						tc.serverAssert(t, r, body)
					}
					w.WriteHeader(tc.serverResp.status)
					_, _ = w.Write([]byte(tc.serverResp.body))
				}))
				defer srv.Close()
			}

			baseURL := ""
			if srv != nil {
				baseURL = srv.URL
			}

			p := newTestProvider(t, baseURL)
			out, err := p.CreatePaymentOrder(t.Context(), model.CreateOrderInput{
				InvoiceID: invoiceID,
				Money:     tc.money,
				MethodID:  tc.methodID,
			})

			switch {
			case tc.wantErr != nil:
				require.ErrorIs(t, err, tc.wantErr)
			case tc.wantErrContain != "":
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrContain)
			default:
				require.NoError(t, err)
				if tc.assertOut != nil {
					tc.assertOut(t, out)
				}
			}
		})
	}
}

func TestProvider_VerifyCallback(t *testing.T) {
	t.Parallel()

	validBody := `{"id":"tx-1","amount":200,"currency":"RUB","status":"CONFIRMED","paymentMethod":2}`
	validHeaders := map[string]string{
		"X-MerchantId": testMerchantID,
		"X-Secret":     testSecret,
	}

	tests := []struct {
		name           string
		headers        map[string]string
		body           string
		wantErr        error
		wantErrContain string
		wantStatus     model.InvoiceStatus
		wantOrderID    string
	}{
		{
			name:        "confirmed",
			headers:     validHeaders,
			body:        validBody,
			wantStatus:  model.SuccessInvoiceStatus,
			wantOrderID: "tx-1",
		},
		{
			name:        "canceled",
			headers:     validHeaders,
			body:        `{"id":"tx-1","amount":200,"currency":"RUB","status":"CANCELED","paymentMethod":2}`,
			wantStatus:  model.CanceledInvoiceStatus,
			wantOrderID: "tx-1",
		},
		{
			name:        "chargebacked_maps_to_canceled",
			headers:     validHeaders,
			body:        `{"id":"tx-1","amount":200,"currency":"RUB","status":"CHARGEBACKED","paymentMethod":2}`,
			wantStatus:  model.CanceledInvoiceStatus,
			wantOrderID: "tx-1",
		},
		{
			name:        "pending_maps_to_unknown",
			headers:     validHeaders,
			body:        `{"id":"tx-1","amount":200,"currency":"RUB","status":"PENDING","paymentMethod":2}`,
			wantStatus:  model.UnknownInvoiceStatus,
			wantOrderID: "tx-1",
		},
		{
			name: "bad_secret",
			headers: map[string]string{
				"X-MerchantId": testMerchantID,
				"X-Secret":     "wrong",
			},
			body:    validBody,
			wantErr: payment.ErrBadSignature,
		},
		{
			name: "bad_merchant_id",
			headers: map[string]string{
				"X-MerchantId": "wrong",
				"X-Secret":     testSecret,
			},
			body:    validBody,
			wantErr: payment.ErrBadSignature,
		},
		{
			name:    "missing_headers",
			headers: nil,
			body:    validBody,
			wantErr: payment.ErrBadSignature,
		},
		{
			name:           "bad_json",
			headers:        validHeaders,
			body:           `not json`,
			wantErrContain: "decode callback",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := newTestProvider(t, "https://app.platega.io")
			out, err := p.VerifyCallback(model.CallbackInput{
				ProviderName: "Platega",
				Headers:      tc.headers,
				Body:         []byte(tc.body),
			})

			switch {
			case tc.wantErr != nil:
				require.True(t, errors.Is(err, tc.wantErr), "expected %v, got %v", tc.wantErr, err)
			case tc.wantErrContain != "":
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErrContain)
			default:
				require.NoError(t, err)
				assert.Equal(t, "Platega", out.ProviderName)
				assert.Equal(t, tc.wantStatus, out.Status)
				assert.Equal(t, tc.wantOrderID, out.PaymentOrderID)
				assert.WithinDuration(t, time.Now().UTC(), out.PaidAt, 5*time.Second)
			}
		})
	}
}

func TestParseExpiresIn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    time.Duration
		wantErr bool
	}{
		{name: "15m", in: "00:15:00", want: 15 * time.Minute},
		{name: "1h_30m_45s", in: "01:30:45", want: time.Hour + 30*time.Minute + 45*time.Second},
		{name: "empty", in: "", wantErr: true},
		{name: "garbage", in: "garbage", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseExpiresIn(tc.in)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
