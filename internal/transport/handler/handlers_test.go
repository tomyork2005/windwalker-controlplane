package handler

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/iotest"

	"control-plane/internal/model"
	"control-plane/internal/payment"
	handlermocks "control-plane/internal/transport/handler/mocks"

	"github.com/gojuno/minimock/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandler_CallbackPlatega(t *testing.T) {
	t.Parallel()

	const (
		merchantID = "merchant-1"
		secret     = "secret-1"
	)

	tests := []struct {
		name           string
		body           io.Reader
		merchantHeader string
		secretHeader   string
		svcErr         error
		assertReq      func(t *testing.T, req model.CallbackInput)
		wantStatus     int
	}{
		{
			name:           "success_returns_200",
			body:           bytes.NewBufferString(`{"id":"tx-1","status":"CONFIRMED"}`),
			merchantHeader: merchantID,
			secretHeader:   secret,
			svcErr:         nil,
			assertReq: func(t *testing.T, req model.CallbackInput) {
				assert.Equal(t, "Platega", req.ProviderName)
				assert.Equal(t, merchantID, req.Headers["X-MerchantId"])
				assert.Equal(t, secret, req.Headers["X-Secret"])
				assert.JSONEq(t, `{"id":"tx-1","status":"CONFIRMED"}`, string(req.Body))
			},
			wantStatus: http.StatusOK,
		},
		{
			name:           "bad_signature_returns_401",
			body:           bytes.NewBufferString(`{}`),
			merchantHeader: "wrong",
			secretHeader:   "wrong",
			svcErr:         payment.ErrBadSignature,
			wantStatus:     http.StatusUnauthorized,
		},
		{
			name:           "transient_error_returns_500",
			body:           bytes.NewBufferString(`{}`),
			merchantHeader: merchantID,
			secretHeader:   secret,
			svcErr:         errors.New("db down"),
			wantStatus:     http.StatusInternalServerError,
		},
		{
			name:           "body_read_error_returns_400",
			body:           iotest.ErrReader(errors.New("boom")),
			merchantHeader: merchantID,
			secretHeader:   secret,
			wantStatus:     http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctrl := minimock.NewController(t)
			svc := handlermocks.NewProcessServiceMock(ctrl)

			expectSvcCall := tc.wantStatus != http.StatusBadRequest
			if expectSvcCall {
				svc.ProcessPaymentCallbackMock.Set(func(_ context.Context, req model.CallbackInput) error {
					if tc.assertReq != nil {
						tc.assertReq(t, req)
					}
					return tc.svcErr
				})
			}

			h := NewHandler(svc)
			req := httptest.NewRequest(http.MethodPost, "/api/control/payment/platega/webhook", tc.body)
			req.Header.Set("X-MerchantId", tc.merchantHeader)
			req.Header.Set("X-Secret", tc.secretHeader)

			rr := httptest.NewRecorder()
			h.CallbackPlatega(rr, req)

			require.Equal(t, tc.wantStatus, rr.Code)
		})
	}
}

func TestHandler_CallbackCryptoCloud_smoke(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		svcErr     error
		wantStatus int
	}{
		{name: "ok", svcErr: nil, wantStatus: http.StatusOK},
		{name: "err_400", svcErr: errors.New("verify failed"), wantStatus: http.StatusBadRequest},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctrl := minimock.NewController(t)
			svc := handlermocks.NewProcessServiceMock(ctrl)
			svc.ProcessPaymentCallbackMock.Return(tc.svcErr)

			h := NewHandler(svc)
			req := httptest.NewRequest(http.MethodPost, "/api/control/payment/cryptocloud/webhook", bytes.NewBufferString(`{}`))
			rr := httptest.NewRecorder()
			h.CallbackCryptoCloud(rr, req)

			require.Equal(t, tc.wantStatus, rr.Code)
		})
	}
}
