package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"control-plane/internal/model"
	"control-plane/internal/payment"
	servicemocks "control-plane/internal/service/mocks"
	store "control-plane/internal/storage"

	"github.com/gojuno/minimock/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessService_ProcessPaymentCallback(t *testing.T) {
	t.Parallel()

	const (
		invoiceID       = "i-1"
		userID          = "u-1"
		planID          = "p-1"
		providerOrderID = "tx-1"
		providerName    = "Platega"
		chatID          = int64(42)
	)

	plan := &model.Plan{
		ID:           planID,
		Region:       "ru",
		DriverType:   "vless",
		DurationDays: 30,
	}

	makeCreatedInvoice := func() *model.Invoice {
		return &model.Invoice{
			ID:              invoiceID,
			ProviderOrderID: providerOrderID,
			UserID:          userID,
			PlanID:          planID,
			ChatID:          chatID,
			PaymentProvider: providerName,
			Status:          model.CreatedInvoiceStatus,
		}
	}

	type mocks struct {
		pay     *servicemocks.ProcessPaymentMock
		storage *servicemocks.ProcessStorageMock
		outbox  *servicemocks.OutboxMock
	}

	tests := []struct {
		name      string
		callback  model.CallbackOutput
		verifyErr error
		setup     func(t *testing.T, m mocks)
		wantErr   error
		wantErrIs error
	}{
		{
			name: "success_first_time",
			callback: model.CallbackOutput{
				ProviderName:   providerName,
				PaymentOrderID: providerOrderID,
				Status:         model.SuccessInvoiceStatus,
				PaidAt:         time.Now().UTC(),
			},
			setup: func(t *testing.T, m mocks) {
				m.storage.WithTxMock.Set(func(ctx context.Context, fn func(ctx context.Context) error) error {
					return fn(ctx)
				})
				m.storage.GetInvoiceByProviderOrderMock.
					Expect(minimock.AnyContext, providerName, providerOrderID).
					Return(makeCreatedInvoice(), nil)
				m.storage.UpdateInvoiceMock.Inspect(func(_ context.Context, inv *model.Invoice) {
					assert.Equal(t, model.SuccessInvoiceStatus, inv.Status)
					assert.False(t, inv.PaidAt.IsZero())
				}).Return(nil)
				m.storage.GetPlanByIDMock.Expect(minimock.AnyContext, planID).Return(plan, nil)
				m.storage.CreateSubscriptionMock.Inspect(func(_ context.Context, sub *model.Subscription) {
					assert.Equal(t, userID, sub.UserID)
					assert.Equal(t, planID, sub.PlanID)
					assert.Equal(t, model.ActiveSubscriptionStatus, sub.Status)
					require.NotNil(t, sub.InvoiceID)
					assert.Equal(t, invoiceID, *sub.InvoiceID)
				}).Return(nil)
				m.outbox.SaveOutboxEventMock.Set(func(_ context.Context, eventType string, _ any) error {
					switch eventType {
					case model.EventTypeInvoicePaidNotification, model.EventTypeSubscriptionActivated:
						return nil
					default:
						t.Fatalf("unexpected event type %q", eventType)
						return nil
					}
				})
			},
		},
		{
			name: "idempotent_already_success",
			callback: model.CallbackOutput{
				ProviderName:   providerName,
				PaymentOrderID: providerOrderID,
				Status:         model.SuccessInvoiceStatus,
			},
			setup: func(t *testing.T, m mocks) {
				m.storage.WithTxMock.Set(func(ctx context.Context, fn func(ctx context.Context) error) error {
					return fn(ctx)
				})
				inv := makeCreatedInvoice()
				inv.Status = model.SuccessInvoiceStatus
				m.storage.GetInvoiceByProviderOrderMock.
					Expect(minimock.AnyContext, providerName, providerOrderID).
					Return(inv, nil)
			},
		},
		{
			name: "canceled_records_only",
			callback: model.CallbackOutput{
				ProviderName:   providerName,
				PaymentOrderID: providerOrderID,
				Status:         model.CanceledInvoiceStatus,
			},
			setup: func(t *testing.T, m mocks) {
				m.storage.WithTxMock.Set(func(ctx context.Context, fn func(ctx context.Context) error) error {
					return fn(ctx)
				})
				m.storage.GetInvoiceByProviderOrderMock.
					Expect(minimock.AnyContext, providerName, providerOrderID).
					Return(makeCreatedInvoice(), nil)
				m.storage.UpdateInvoiceMock.Inspect(func(_ context.Context, inv *model.Invoice) {
					assert.Equal(t, model.CanceledInvoiceStatus, inv.Status)
				}).Return(nil)
			},
		},
		{
			name: "unknown_status_records_only",
			callback: model.CallbackOutput{
				ProviderName:   providerName,
				PaymentOrderID: providerOrderID,
				Status:         model.UnknownInvoiceStatus,
			},
			setup: func(t *testing.T, m mocks) {
				m.storage.WithTxMock.Set(func(ctx context.Context, fn func(ctx context.Context) error) error {
					return fn(ctx)
				})
				m.storage.GetInvoiceByProviderOrderMock.
					Expect(minimock.AnyContext, providerName, providerOrderID).
					Return(makeCreatedInvoice(), nil)
				m.storage.UpdateInvoiceMock.Inspect(func(_ context.Context, inv *model.Invoice) {
					assert.Equal(t, model.UnknownInvoiceStatus, inv.Status)
				}).Return(nil)
			},
		},
		{
			name: "not_found_silently_ok",
			callback: model.CallbackOutput{
				ProviderName:   providerName,
				PaymentOrderID: "tx-missing",
				Status:         model.SuccessInvoiceStatus,
			},
			setup: func(t *testing.T, m mocks) {
				m.storage.WithTxMock.Set(func(ctx context.Context, fn func(ctx context.Context) error) error {
					return fn(ctx)
				})
				m.storage.GetInvoiceByProviderOrderMock.
					Expect(minimock.AnyContext, providerName, "tx-missing").
					Return(nil, store.ErrNotFound)
			},
		},
		{
			name: "empty_provider_order_id",
			callback: model.CallbackOutput{
				ProviderName:   providerName,
				PaymentOrderID: "",
				Status:         model.SuccessInvoiceStatus,
			},
			setup:   func(t *testing.T, m mocks) {}, // no calls
			wantErr: errors.New("empty provider order id"),
		},
		{
			name:      "verify_callback_fails",
			verifyErr: errors.New("decode failed"),
			setup:     func(t *testing.T, m mocks) {},
			wantErr:   errors.New("decode failed"),
		},
		{
			name:      "bad_signature_propagates",
			verifyErr: payment.ErrBadSignature,
			setup:     func(t *testing.T, m mocks) {},
			wantErrIs: payment.ErrBadSignature,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctrl := minimock.NewController(t)
			payMock := servicemocks.NewProcessPaymentMock(ctrl)
			storageMock := servicemocks.NewProcessStorageMock(ctrl)
			outboxMock := servicemocks.NewOutboxMock(ctrl)

			payMock.VerifyCallbackMock.Set(func(_ model.CallbackInput) (model.CallbackOutput, error) {
				if tc.verifyErr != nil {
					return model.CallbackOutput{}, tc.verifyErr
				}
				return tc.callback, nil
			})

			tc.setup(t, mocks{pay: payMock, storage: storageMock, outbox: outboxMock})

			svc := NewProcessService(payMock, storageMock, outboxMock)
			err := svc.ProcessPaymentCallback(t.Context(), model.CallbackInput{
				ProviderName: providerName,
				Body:         []byte(`{}`),
			})

			switch {
			case tc.wantErrIs != nil:
				require.ErrorIs(t, err, tc.wantErrIs)
			case tc.wantErr != nil:
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr.Error())
			default:
				require.NoError(t, err)
			}
		})
	}
}
