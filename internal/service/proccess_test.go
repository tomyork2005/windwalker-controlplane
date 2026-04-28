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
		{
			name: "renewal_active_extends_end_at_and_emits_three_events",
			callback: model.CallbackOutput{
				ProviderName:   providerName,
				PaymentOrderID: providerOrderID,
				Status:         model.SuccessInvoiceStatus,
				PaidAt:         time.Now().UTC(),
			},
			setup: func(t *testing.T, m mocks) {
				const subID = "sub-renew"
				const agentID = "agent-1"
				renewID := subID
				inv := makeCreatedInvoice()
				inv.RenewsSubscriptionID = &renewID

				existingEnd := time.Now().UTC().Add(48 * time.Hour)
				existingAgent := agentID
				sub := &model.SubscriptionWithPlan{
					Subscription: model.Subscription{
						ID:      subID,
						UserID:  userID,
						AgentID: &existingAgent,
						ChatID:  chatID,
						Status:  model.ActiveSubscriptionStatus,
						EndAt:   existingEnd,
					},
					Region:       "ru",
					DriverType:   "vless",
					DurationDays: 30,
				}
				expectedNewEnd := existingEnd.AddDate(0, 0, int(plan.DurationDays))

				m.storage.WithTxMock.Set(func(ctx context.Context, fn func(ctx context.Context) error) error {
					return fn(ctx)
				})
				m.storage.GetInvoiceByProviderOrderMock.
					Expect(minimock.AnyContext, providerName, providerOrderID).
					Return(inv, nil)
				m.storage.UpdateInvoiceMock.Inspect(func(_ context.Context, got *model.Invoice) {
					assert.Equal(t, model.SuccessInvoiceStatus, got.Status)
				}).Return(nil)
				m.storage.GetSubscriptionByIDForUpdateMock.
					Expect(minimock.AnyContext, subID).
					Return(sub, nil)
				m.storage.GetPlanByIDMock.Expect(minimock.AnyContext, planID).Return(plan, nil)
				m.storage.ExtendSubscriptionEndAtMock.Inspect(func(_ context.Context, gotID string, gotEnd time.Time) {
					assert.Equal(t, subID, gotID)
					assert.True(t, gotEnd.Equal(expectedNewEnd), "new end_at = old + plan.DurationDays")
				}).Return(nil)

				seen := map[string]int{}
				m.outbox.SaveOutboxEventMock.Set(func(_ context.Context, eventType string, payload any) error {
					seen[eventType]++
					switch eventType {
					case model.EventTypeInvoicePaidNotification:
					case model.EventTypeSubscriptionRenewed:
						ev, ok := payload.(model.SubscriptionRenewedEvent)
						require.True(t, ok)
						assert.Equal(t, subID, ev.SubscriptionID)
						assert.Equal(t, agentID, ev.AgentID)
						assert.True(t, ev.NewEndAt.Equal(expectedNewEnd))
					case model.EventTypeSubscriptionRenewedNotification:
						ev, ok := payload.(model.SubscriptionRenewedNotificationEvent)
						require.True(t, ok)
						assert.Equal(t, chatID, ev.ChatID)
						assert.True(t, ev.NewEndAt.Equal(expectedNewEnd))
					default:
						t.Fatalf("unexpected event type %q", eventType)
					}
					return nil
				})
				t.Cleanup(func() {
					assert.Equal(t, 1, seen[model.EventTypeInvoicePaidNotification])
					assert.Equal(t, 1, seen[model.EventTypeSubscriptionRenewed])
					assert.Equal(t, 1, seen[model.EventTypeSubscriptionRenewedNotification])
				})
			},
		},
		{
			name: "renewal_inactive_returns_ErrSubscriptionExpired",
			callback: model.CallbackOutput{
				ProviderName:   providerName,
				PaymentOrderID: providerOrderID,
				Status:         model.SuccessInvoiceStatus,
				PaidAt:         time.Now().UTC(),
			},
			setup: func(t *testing.T, m mocks) {
				const subID = "sub-renew-inactive"
				renewID := subID
				inv := makeCreatedInvoice()
				inv.RenewsSubscriptionID = &renewID

				agent := "agent-1"
				sub := &model.SubscriptionWithPlan{
					Subscription: model.Subscription{
						ID:      subID,
						UserID:  userID,
						AgentID: &agent,
						ChatID:  chatID,
						Status:  model.InactiveSubscriptionStatus,
						EndAt:   time.Now().UTC().Add(-time.Hour),
					},
				}

				m.storage.WithTxMock.Set(func(ctx context.Context, fn func(ctx context.Context) error) error {
					return fn(ctx)
				})
				m.storage.GetInvoiceByProviderOrderMock.
					Expect(minimock.AnyContext, providerName, providerOrderID).
					Return(inv, nil)
				m.storage.UpdateInvoiceMock.Return(nil)
				m.outbox.SaveOutboxEventMock.Set(func(_ context.Context, eventType string, _ any) error {
					// invoice_paid_notification кладётся ДО ветвления — это ОК; всё в одной tx,
					// которая откатится по ErrSubscriptionExpired, поэтому событие фактически не сохранится.
					assert.Equal(t, model.EventTypeInvoicePaidNotification, eventType)
					return nil
				})
				m.storage.GetSubscriptionByIDForUpdateMock.
					Expect(minimock.AnyContext, subID).
					Return(sub, nil)
			},
			wantErrIs: ErrSubscriptionExpired,
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
