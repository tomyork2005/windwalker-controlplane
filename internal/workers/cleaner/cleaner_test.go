package cleaner

import (
	"context"
	workersmocks "control-plane/internal/workers/cleaner/mocks"
	"errors"
	"testing"
	"time"

	"control-plane/internal/model"

	"github.com/gojuno/minimock/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ptrStr(s string) *string { return &s }

type capturedEvent struct {
	eventType string
	cancel    *model.SubscriptionCancelEvent
	expired   *model.SubscriptionExpiredNotificationEvent
	expiring  *model.SubscriptionExpiringNotificationEvent
}

func makeSub(id, userID, driverType string, agentID *string) *model.SubscriptionWithPlan {
	return &model.SubscriptionWithPlan{
		Subscription: model.Subscription{
			ID:      id,
			UserID:  userID,
			AgentID: agentID,
			ChatID:  100,
			Status:  model.ActiveSubscriptionStatus,
		},
		DriverType: driverType,
	}
}

func makeWarnSub(id string, chatID int64, endAt time.Time) *model.SubscriptionWithPlan {
	return &model.SubscriptionWithPlan{
		Subscription: model.Subscription{
			ID:     id,
			UserID: "u-" + id,
			ChatID: chatID,
			Status: model.ActiveSubscriptionStatus,
			EndAt:  endAt,
		},
	}
}

func TestCleaner_processExpired(t *testing.T) {
	t.Parallel()

	errBoom := errors.New("boom")

	expiredEvent := func(id string, chatID int64) capturedEvent {
		return capturedEvent{
			eventType: model.EventTypeSubscriptionExpiredNotification,
			expired: &model.SubscriptionExpiredNotificationEvent{
				SubscriptionID: id,
				ChatID:         chatID,
			},
		}
	}
	cancelEvent := func(id, userID, agentID, driverType string) capturedEvent {
		return capturedEvent{
			eventType: model.EventTypeSubscriptionCancelled,
			cancel: &model.SubscriptionCancelEvent{
				UserID:         userID,
				AgentID:        agentID,
				DriverType:     driverType,
				SubscriptionID: id,
			},
		}
	}

	tests := []struct {
		name           string
		subs           []*model.SubscriptionWithPlan
		listErr        error
		markErrByID    map[string]error
		saveErrAtIndex map[int]error
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
				expiredEvent("sub-1", 100),
				cancelEvent("sub-1", "u-1", "agent-1", "vless"),
			},
		},
		{
			name: "single_expired_no_agent",
			subs: []*model.SubscriptionWithPlan{
				makeSub("sub-2", "u-2", "vmess", nil),
			},
			wantMarked: []string{"sub-2"},
			wantEvents: []capturedEvent{
				expiredEvent("sub-2", 100),
			},
		},
		{
			name: "single_expired_empty_string_agent",
			subs: []*model.SubscriptionWithPlan{
				makeSub("sub-3", "u-3", "vless", ptrStr("")),
			},
			wantMarked: []string{"sub-3"},
			wantEvents: []capturedEvent{
				expiredEvent("sub-3", 100),
			},
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
				expiredEvent("sub-a", 100),
				cancelEvent("sub-a", "u-a", "agent-1", "vless"),
				expiredEvent("sub-b", 100),
				expiredEvent("sub-c", 100),
				cancelEvent("sub-c", "u-c", "agent-2", "vless"),
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
				expiredEvent("sub-1", 100),
				cancelEvent("sub-1", "u-1", "agent-1", "vless"),
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
				expiredEvent("sub-1", 100),
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
				idx := len(captured)
				ev := capturedEvent{eventType: eventType}
				switch p := payload.(type) {
				case model.SubscriptionCancelEvent:
					ev.cancel = &p
				case model.SubscriptionExpiredNotificationEvent:
					ev.expired = &p
				case model.SubscriptionExpiringNotificationEvent:
					ev.expiring = &p
				default:
					require.Failf(t, "unexpected payload", "got %T", payload)
				}
				captured = append(captured, ev)
				if tc.saveErrAtIndex != nil {
					if err, ok := tc.saveErrAtIndex[idx]; ok {
						return err
					}
				}
				return nil
			})

			c := NewCleaner(storageMock, nil)
			err := c.processExpired(t.Context())

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

type listCall struct {
	threshold time.Duration
}

type warnedCall struct {
	id string
}

type expiringEvent struct {
	subscriptionID string
	chatID         int64
	remainingHours float64
}

func TestCleaner_processWarnings(t *testing.T) {
	t.Parallel()

	errBoom := errors.New("boom")
	now := time.Now()

	tests := []struct {
		name              string
		warnings          []time.Duration
		listByThreshold   map[time.Duration][]*model.SubscriptionWithPlan
		listErrByIndex    map[int]error
		markWarnedErrByID map[string]error
		saveErrAtIndex    map[int]error
		wantListOrder     []time.Duration
		wantWarned        []warnedCall
		wantEvents        []expiringEvent
	}{
		{
			name:          "no_warnings_configured",
			warnings:      nil,
			wantListOrder: nil,
			wantWarned:    nil,
			wantEvents:    nil,
		},
		{
			name:     "no_subs_in_any_window",
			warnings: []time.Duration{3 * time.Hour, 24 * time.Hour},
			listByThreshold: map[time.Duration][]*model.SubscriptionWithPlan{
				3 * time.Hour:  nil,
				24 * time.Hour: nil,
			},
			wantListOrder: []time.Duration{3 * time.Hour, 24 * time.Hour},
			wantWarned:    nil,
			wantEvents:    nil,
		},
		{
			name:     "single_sub_in_3h_only",
			warnings: []time.Duration{3 * time.Hour, 24 * time.Hour},
			listByThreshold: map[time.Duration][]*model.SubscriptionWithPlan{
				3 * time.Hour:  {makeWarnSub("A", 1001, now.Add(2*time.Hour))},
				24 * time.Hour: nil,
			},
			wantListOrder: []time.Duration{3 * time.Hour, 24 * time.Hour},
			wantWarned:    []warnedCall{{id: "A"}},
			wantEvents: []expiringEvent{
				{subscriptionID: "A", chatID: 1001, remainingHours: 2},
			},
		},
		{
			name:     "single_sub_in_24h_only",
			warnings: []time.Duration{3 * time.Hour, 24 * time.Hour},
			listByThreshold: map[time.Duration][]*model.SubscriptionWithPlan{
				3 * time.Hour:  nil,
				24 * time.Hour: {makeWarnSub("B", 1002, now.Add(10*time.Hour))},
			},
			wantListOrder: []time.Duration{3 * time.Hour, 24 * time.Hour},
			wantWarned:    []warnedCall{{id: "B"}},
			wantEvents: []expiringEvent{
				{subscriptionID: "B", chatID: 1002, remainingHours: 10},
			},
		},
		{
			name:     "multiple_subs_one_threshold",
			warnings: []time.Duration{3 * time.Hour},
			listByThreshold: map[time.Duration][]*model.SubscriptionWithPlan{
				3 * time.Hour: {
					makeWarnSub("H", 1003, now.Add(1*time.Hour)),
					makeWarnSub("I", 1004, now.Add(2*time.Hour)),
				},
			},
			wantListOrder: []time.Duration{3 * time.Hour},
			wantWarned:    []warnedCall{{id: "H"}, {id: "I"}},
			wantEvents: []expiringEvent{
				{subscriptionID: "H", chatID: 1003, remainingHours: 1},
				{subscriptionID: "I", chatID: 1004, remainingHours: 2},
			},
		},
		{
			name:     "list_error_in_first_threshold_skips_to_next",
			warnings: []time.Duration{3 * time.Hour, 24 * time.Hour},
			listByThreshold: map[time.Duration][]*model.SubscriptionWithPlan{
				24 * time.Hour: {makeWarnSub("D", 1005, now.Add(10*time.Hour))},
			},
			listErrByIndex: map[int]error{0: errBoom},
			wantListOrder:  []time.Duration{3 * time.Hour, 24 * time.Hour},
			wantWarned:     []warnedCall{{id: "D"}},
			wantEvents: []expiringEvent{
				{subscriptionID: "D", chatID: 1005, remainingHours: 10},
			},
		},
		{
			name:     "mark_warned_fails_within_tx",
			warnings: []time.Duration{3 * time.Hour},
			listByThreshold: map[time.Duration][]*model.SubscriptionWithPlan{
				3 * time.Hour: {makeWarnSub("E", 1006, now.Add(2*time.Hour))},
			},
			markWarnedErrByID: map[string]error{"E": errBoom},
			wantListOrder:     []time.Duration{3 * time.Hour},
			wantWarned:        []warnedCall{{id: "E"}},
			wantEvents:        nil,
		},
		{
			name:     "save_outbox_fails_within_tx",
			warnings: []time.Duration{3 * time.Hour},
			listByThreshold: map[time.Duration][]*model.SubscriptionWithPlan{
				3 * time.Hour: {
					makeWarnSub("F", 1007, now.Add(1*time.Hour)),
					makeWarnSub("G", 1008, now.Add(2*time.Hour)),
				},
			},
			saveErrAtIndex: map[int]error{0: errBoom},
			wantListOrder:  []time.Duration{3 * time.Hour},
			wantWarned:     []warnedCall{{id: "F"}},
			wantEvents: []expiringEvent{
				{subscriptionID: "F", chatID: 1007, remainingHours: 1},
			},
		},
		{
			name:     "warnings_unsorted_normalized_to_asc",
			warnings: []time.Duration{24 * time.Hour, 3 * time.Hour},
			listByThreshold: map[time.Duration][]*model.SubscriptionWithPlan{
				3 * time.Hour:  nil,
				24 * time.Hour: nil,
			},
			wantListOrder: []time.Duration{3 * time.Hour, 24 * time.Hour},
			wantWarned:    nil,
			wantEvents:    nil,
		},
		{
			name:     "warnings_with_zero_and_negative_filtered",
			warnings: []time.Duration{0, -1 * time.Hour, 3 * time.Hour},
			listByThreshold: map[time.Duration][]*model.SubscriptionWithPlan{
				3 * time.Hour: nil,
			},
			wantListOrder: []time.Duration{3 * time.Hour},
			wantWarned:    nil,
			wantEvents:    nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctrl := minimock.NewController(t)
			storageMock := workersmocks.NewCleanerStorageMock(ctrl)

			storageMock.WithTxMock.Optional().Set(func(ctx context.Context, fn func(ctx context.Context) error) error {
				return fn(ctx)
			})

			var listCalls []listCall
			storageMock.ListSubsAboutToExpireMock.Optional().Set(func(_ context.Context, threshold time.Duration, limit int) ([]*model.SubscriptionWithPlan, error) {
				idx := len(listCalls)
				listCalls = append(listCalls, listCall{threshold: threshold})
				assert.Equal(t, cleanerBatchSize, limit, "limit must equal cleanerBatchSize")
				if tc.listErrByIndex != nil {
					if err, ok := tc.listErrByIndex[idx]; ok {
						return nil, err
					}
				}
				return tc.listByThreshold[threshold], nil
			})

			var warned []warnedCall
			storageMock.MarkSubscriptionWarnedMock.Optional().Set(func(_ context.Context, id string) error {
				warned = append(warned, warnedCall{id: id})
				if tc.markWarnedErrByID != nil {
					if err := tc.markWarnedErrByID[id]; err != nil {
						return err
					}
				}
				return nil
			})

			var events []expiringEvent
			storageMock.SaveOutboxEventMock.Optional().Set(func(_ context.Context, eventType string, payload any) error {
				idx := len(events)
				assert.Equal(t, model.EventTypeSubscriptionExpiringNotification, eventType)
				ev, ok := payload.(model.SubscriptionExpiringNotificationEvent)
				require.True(t, ok, "expected SubscriptionExpiringNotificationEvent payload, got %T", payload)
				events = append(events, expiringEvent{
					subscriptionID: ev.SubscriptionID,
					chatID:         ev.ChatID,
					remainingHours: ev.RemainingDuration.Hours(),
				})
				if tc.saveErrAtIndex != nil {
					if err, ok := tc.saveErrAtIndex[idx]; ok {
						return err
					}
				}
				return nil
			})

			c := NewCleaner(storageMock, tc.warnings)
			err := c.processWarnings(t.Context())
			require.NoError(t, err, "processWarnings must swallow per-threshold errors")

			var gotOrder []time.Duration
			for _, call := range listCalls {
				gotOrder = append(gotOrder, call.threshold)
			}
			assert.Equal(t, tc.wantListOrder, gotOrder, "ListSubsAboutToExpire threshold order")
			assert.Equal(t, tc.wantWarned, warned, "MarkSubscriptionWarned call sequence")

			require.Equal(t, len(tc.wantEvents), len(events), "events count")
			for i, want := range tc.wantEvents {
				got := events[i]
				assert.Equal(t, want.subscriptionID, got.subscriptionID, "event[%d].subscriptionID", i)
				assert.Equal(t, want.chatID, got.chatID, "event[%d].chatID", i)
				assert.InDelta(t, want.remainingHours, got.remainingHours, 0.05, "event[%d].remainingHours", i)
			}
		})
	}
}
