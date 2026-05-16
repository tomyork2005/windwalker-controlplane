package workers

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"control-plane/internal/model"
	workermocks "control-plane/internal/workers/mocks"

	"github.com/gojuno/minimock/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type savedEvent struct {
	eventType string
	failed    *model.DeliveryFailedNotificationEvent
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

func TestMaybeEscalate(t *testing.T) {
	t.Parallel()

	credsPayload := mustJSON(t, model.CredsDeliveryEvent{
		SubscriptionID: "sub-creds",
		ChatID:         42,
		Message:        "vless://...",
	})
	activatedWithChat := mustJSON(t, model.SubscriptionActivatedEvent{
		SubscriptionID: "sub-act",
		UserID:         "u-act",
		ChatID:         99,
	})
	activatedNoChat := mustJSON(t, model.SubscriptionActivatedEvent{
		SubscriptionID: "sub-act-legacy",
		UserID:         "u-legacy",
		ChatID:         0,
	})
	invoicePaid := mustJSON(t, model.InvoicePaidNotificationEvent{ChatID: 50})

	tests := []struct {
		name      string
		event     model.OutboxEvent
		wantSaved []savedEvent
	}{
		{
			name: "creds_delivery_exhausted_emits_delivery_failed",
			event: model.OutboxEvent{
				EventType: model.EventTypeCredsDelivery,
				Attempts:  maxProcessAttempts - 1,
				Payload:   credsPayload,
			},
			wantSaved: []savedEvent{{
				eventType: model.EventTypeDeliveryFailedNotification,
				failed:    &model.DeliveryFailedNotificationEvent{SubscriptionID: "sub-creds", ChatID: 42},
			}},
		},
		{
			name: "subscription_activated_exhausted_with_chat_id_emits_delivery_failed",
			event: model.OutboxEvent{
				EventType: model.EventTypeSubscriptionActivated,
				Attempts:  maxProcessAttempts - 1,
				Payload:   activatedWithChat,
			},
			wantSaved: []savedEvent{{
				eventType: model.EventTypeDeliveryFailedNotification,
				failed:    &model.DeliveryFailedNotificationEvent{SubscriptionID: "sub-act", ChatID: 99},
			}},
		},
		{
			name: "subscription_activated_exhausted_without_chat_id_skips",
			event: model.OutboxEvent{
				EventType: model.EventTypeSubscriptionActivated,
				Attempts:  maxProcessAttempts - 1,
				Payload:   activatedNoChat,
			},
			wantSaved: nil,
		},
		{
			name: "subscription_activated_not_yet_exhausted_no_op",
			event: model.OutboxEvent{
				EventType: model.EventTypeSubscriptionActivated,
				Attempts:  0,
				Payload:   activatedWithChat,
			},
			wantSaved: nil,
		},
		{
			name: "creds_delivery_not_yet_exhausted_no_op",
			event: model.OutboxEvent{
				EventType: model.EventTypeCredsDelivery,
				Attempts:  maxProcessAttempts - 2,
				Payload:   credsPayload,
			},
			wantSaved: nil,
		},
		{
			name: "unrelated_event_type_no_op",
			event: model.OutboxEvent{
				EventType: model.EventTypeInvoicePaidNotification,
				Attempts:  maxProcessAttempts - 1,
				Payload:   invoicePaid,
			},
			wantSaved: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctrl := minimock.NewController(t)
			storageMock := workermocks.NewStorageMock(ctrl)

			var saved []savedEvent
			storageMock.SaveOutboxEventMock.Optional().Set(func(_ context.Context, eventType string, payload any) error {
				ev := savedEvent{eventType: eventType}
				switch p := payload.(type) {
				case model.DeliveryFailedNotificationEvent:
					ev.failed = &p
				default:
					require.Failf(t, "unexpected payload", "got %T", payload)
				}
				saved = append(saved, ev)
				return nil
			})

			w := NewWorker(storageMock, nil, nil)
			w.maybeEscalate(t.Context(), tc.event, errors.New("cause"))

			assert.Equal(t, tc.wantSaved, saved, "SaveOutboxEvent call sequence + payload")
		})
	}
}
