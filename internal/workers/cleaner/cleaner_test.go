package cleaner

import (
	"context"
	workersmocks "control-plane/internal/workers/cleaner/mocks"
	"errors"
	"testing"

	"control-plane/internal/model"

	"github.com/gojuno/minimock/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ptrStr(s string) *string { return &s }

type capturedEvent struct {
	eventType string
	payload   model.SubscriptionCancelEvent
}

func makeSub(id, userID, driverType string, agentID *string) *model.SubscriptionWithPlan {
	return &model.SubscriptionWithPlan{
		Subscription: model.Subscription{
			ID:      id,
			UserID:  userID,
			AgentID: agentID,
			Status:  model.ActiveSubscriptionStatus,
		},
		DriverType: driverType,
	}
}

func TestCleaner_processBatch(t *testing.T) {
	t.Parallel()

	errBoom := errors.New("boom")

	tests := []struct {
		name           string
		subs           []*model.SubscriptionWithPlan
		listErr        error
		markErrByID    map[string]error
		saveErrAtIndex map[int]error
		setupExtra     func(t *testing.T, storageMock *workersmocks.CleanerStorageMock)
		wantErr        bool
		wantErrIs      error
		wantMarked     []string
		wantEvents     []capturedEvent
	}{
		{
			name:       "no_expired",
			subs:       nil,
			wantMarked: nil,
			wantEvents: nil,
		},
		{
			name: "single_expired_with_agent",
			subs: []*model.SubscriptionWithPlan{
				makeSub("sub-1", "u-1", "vless", ptrStr("agent-1")),
			},
			wantMarked: []string{"sub-1"},
			wantEvents: []capturedEvent{
				{
					eventType: model.EventTypeSubscriptionCancelled,
					payload: model.SubscriptionCancelEvent{
						UserID:         "u-1",
						AgentID:        "agent-1",
						DriverType:     "vless",
						SubscriptionID: "sub-1",
					},
				},
			},
		},
		{
			name: "single_expired_no_agent",
			subs: []*model.SubscriptionWithPlan{
				makeSub("sub-2", "u-2", "vmess", nil),
			},
			wantMarked: []string{"sub-2"},
			wantEvents: nil,
		},
		{
			name: "single_expired_empty_string_agent",
			subs: []*model.SubscriptionWithPlan{
				makeSub("sub-3", "u-3", "vless", ptrStr("")),
			},
			wantMarked: []string{"sub-3"},
			wantEvents: nil,
		},
		{
			name: "batch_with_mixed_agent_state",
			subs: []*model.SubscriptionWithPlan{
				makeSub("sub-a", "u-a", "vless", ptrStr("agent-1")),
				makeSub("sub-b", "u-b", "vmess", nil),
				makeSub("sub-c", "u-c", "vless", ptrStr("agent-2")),
			},
			wantMarked: []string{"sub-a", "sub-b", "sub-c"},
			wantEvents: []capturedEvent{
				{
					eventType: model.EventTypeSubscriptionCancelled,
					payload: model.SubscriptionCancelEvent{
						UserID:         "u-a",
						AgentID:        "agent-1",
						DriverType:     "vless",
						SubscriptionID: "sub-a",
					},
				},
				{
					eventType: model.EventTypeSubscriptionCancelled,
					payload: model.SubscriptionCancelEvent{
						UserID:         "u-c",
						AgentID:        "agent-2",
						DriverType:     "vless",
						SubscriptionID: "sub-c",
					},
				},
			},
		},
		{
			name:       "list_returns_error",
			listErr:    errBoom,
			wantErr:    true,
			wantErrIs:  errBoom,
			wantMarked: nil,
			wantEvents: nil,
		},
		{
			name: "mark_inactive_fails_aborts_batch",
			subs: []*model.SubscriptionWithPlan{
				makeSub("sub-1", "u-1", "vless", ptrStr("agent-1")),
				makeSub("sub-2", "u-2", "vless", ptrStr("agent-2")),
				makeSub("sub-3", "u-3", "vless", ptrStr("agent-3")),
			},
			markErrByID: map[string]error{"sub-2": errBoom},
			wantErr:     true,
			wantErrIs:   errBoom,
			wantMarked:  []string{"sub-1", "sub-2"},
			wantEvents: []capturedEvent{
				{
					eventType: model.EventTypeSubscriptionCancelled,
					payload: model.SubscriptionCancelEvent{
						UserID:         "u-1",
						AgentID:        "agent-1",
						DriverType:     "vless",
						SubscriptionID: "sub-1",
					},
				},
			},
		},
		{
			name: "save_outbox_fails_aborts_batch",
			subs: []*model.SubscriptionWithPlan{
				makeSub("sub-1", "u-1", "vless", ptrStr("agent-1")),
				makeSub("sub-2", "u-2", "vless", ptrStr("agent-2")),
			},
			saveErrAtIndex: map[int]error{0: errBoom},
			wantErr:        true,
			wantErrIs:      errBoom,
			wantMarked:     []string{"sub-1"},
			wantEvents: []capturedEvent{
				{
					eventType: model.EventTypeSubscriptionCancelled,
					payload: model.SubscriptionCancelEvent{
						UserID:         "u-1",
						AgentID:        "agent-1",
						DriverType:     "vless",
						SubscriptionID: "sub-1",
					},
				},
			},
		},
		{
			name: "with_tx_propagates_callback_error",
			subs: []*model.SubscriptionWithPlan{
				makeSub("sub-1", "u-1", "vless", ptrStr("agent-1")),
			},
			markErrByID: map[string]error{"sub-1": errBoom},
			wantErr:     true,
			wantErrIs:   errBoom,
			wantMarked:  []string{"sub-1"},
			wantEvents:  nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctrl := minimock.NewController(t)
			storageMock := workersmocks.NewCleanerStorageMock(ctrl)

			storageMock.WithTxMock.Set(func(ctx context.Context, fn func(ctx context.Context) error) error {
				return fn(ctx)
			})

			storageMock.ListExpiredActiveSubsMock.
				Expect(minimock.AnyContext, cleanerBatchSize).
				Return(tc.subs, tc.listErr)

			var marked []string
			storageMock.MarkSubscriptionInactiveMock.Optional().Set(func(_ context.Context, id string) error {
				marked = append(marked, id)
				if tc.markErrByID != nil {
					if err := tc.markErrByID[id]; err != nil {
						return err
					}
				}
				return nil
			})

			var captured []capturedEvent
			storageMock.SaveOutboxEventMock.Optional().Set(func(_ context.Context, eventType string, payload any) error {
				cancel, ok := payload.(model.SubscriptionCancelEvent)
				require.True(t, ok, "expected SubscriptionCancelEvent payload, got %T", payload)
				idx := len(captured)
				captured = append(captured, capturedEvent{eventType: eventType, payload: cancel})
				if tc.saveErrAtIndex != nil {
					if err, ok := tc.saveErrAtIndex[idx]; ok {
						return err
					}
				}
				return nil
			})

			if tc.setupExtra != nil {
				tc.setupExtra(t, storageMock)
			}

			c := NewCleaner(storageMock)
			err := c.processBatch(t.Context())

			switch {
			case tc.wantErrIs != nil:
				require.Error(t, err)
				require.ErrorIs(t, err, tc.wantErrIs)
			case tc.wantErr:
				require.Error(t, err)
			default:
				require.NoError(t, err)
			}

			assert.Equal(t, tc.wantMarked, marked, "MarkSubscriptionInactive call sequence")
			assert.Equal(t, tc.wantEvents, captured, "SaveOutboxEvent call sequence + payload")

			if tc.listErr != nil {
				assert.Equal(t, uint64(0), storageMock.MarkSubscriptionInactiveAfterCounter(),
					"MarkSubscriptionInactive must not be called when ListExpiredActiveSubs fails")
				assert.Equal(t, uint64(0), storageMock.SaveOutboxEventAfterCounter(),
					"SaveOutboxEvent must not be called when ListExpiredActiveSubs fails")
			}
		})
	}
}
