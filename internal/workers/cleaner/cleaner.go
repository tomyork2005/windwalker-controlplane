package cleaner

import (
	"context"
	"control-plane/internal/model"
	"log/slog"
	"slices"
	"time"
)

const (
	cleanerTickPeriod = 5 * time.Minute
	cleanerBatchSize  = 10
)

type CleanerStorage interface {
	ListExpiredActiveSubs(ctx context.Context, limit int) ([]*model.SubscriptionWithPlan, error)
	MarkSubscriptionInactive(ctx context.Context, id string) error
	ListSubsAboutToExpire(ctx context.Context, threshold time.Duration, limit int) ([]*model.SubscriptionWithPlan, error)
	MarkSubscriptionWarned(ctx context.Context, id string) error
	SaveOutboxEvent(ctx context.Context, eventType string, payload any) error
	WithTx(ctx context.Context, fn func(ctx context.Context) error) error
}

type Cleaner struct {
	storage  CleanerStorage
	warnings []time.Duration
}

func NewCleaner(storage CleanerStorage, warnings []time.Duration) *Cleaner {
	return &Cleaner{
		storage:  storage,
		warnings: normalizeWarnings(warnings),
	}
}

// normalizeWarnings drops non-positive thresholds, deduplicates and sorts
// ascending. Ascending order matters: the smallest threshold is processed
// first so that a subscription falling into multiple windows gets the
// closest-to-end warning, and the larger windows then skip it via the
// last_warning_at predicate.
func normalizeWarnings(in []time.Duration) []time.Duration {
	if len(in) == 0 {
		return nil
	}

	out := make([]time.Duration, 0, len(in))
	seen := make(map[time.Duration]struct{}, len(in))
	for _, d := range in {
		if d <= 0 {
			continue
		}
		if _, ok := seen[d]; ok {
			continue
		}
		seen[d] = struct{}{}
		out = append(out, d)
	}

	slices.Sort(out)
	return out
}

func (c *Cleaner) Run(ctx context.Context) {
	ticker := time.NewTicker(cleanerTickPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.processBatch(ctx)
		}
	}
}

func (c *Cleaner) processBatch(ctx context.Context) {
	if err := c.processExpired(ctx); err != nil {
		slog.Error("cleaner: processExpired failed", "err", err)
	}
	if err := c.processWarnings(ctx); err != nil {
		slog.Error("cleaner: processWarnings failed", "err", err)
	}
}

func (c *Cleaner) processExpired(ctx context.Context) error {
	return c.storage.WithTx(ctx, func(ctx context.Context) error {
		subs, err := c.storage.ListExpiredActiveSubs(ctx, cleanerBatchSize)
		if err != nil {
			return err
		}

		for _, sub := range subs {
			if err = c.storage.MarkSubscriptionInactive(ctx, sub.ID); err != nil {
				return err
			}

			expired := model.SubscriptionExpiredNotificationEvent{
				SubscriptionID: sub.ID,
				ChatID:         sub.ChatID,
			}
			if err = c.storage.SaveOutboxEvent(ctx, model.EventTypeSubscriptionExpiredNotification, expired); err != nil {
				return err
			}

			if sub.AgentID == nil || *sub.AgentID == "" {
				continue
			}

			cancel := model.SubscriptionCancelEvent{
				UserID:         sub.UserID,
				AgentID:        *sub.AgentID,
				DriverType:     sub.DriverType,
				SubscriptionID: sub.ID,
			}
			if err = c.storage.SaveOutboxEvent(ctx, model.EventTypeSubscriptionCancelled, cancel); err != nil {
				return err
			}
		}

		return nil
	})
}

func (c *Cleaner) processWarnings(ctx context.Context) error {
	for _, threshold := range c.warnings {
		err := c.storage.WithTx(ctx, func(ctx context.Context) error {
			subs, err := c.storage.ListSubsAboutToExpire(ctx, threshold, cleanerBatchSize)
			if err != nil {
				return err
			}

			for _, sub := range subs {
				if err = c.storage.MarkSubscriptionWarned(ctx, sub.ID); err != nil {
					return err
				}

				event := model.SubscriptionExpiringNotificationEvent{
					SubscriptionID:    sub.ID,
					ChatID:            sub.ChatID,
					RemainingDuration: time.Until(sub.EndAt),
				}
				if err = c.storage.SaveOutboxEvent(ctx, model.EventTypeSubscriptionExpiringNotification, event); err != nil {
					return err
				}
			}

			return nil
		})
		if err != nil {
			slog.Error("cleaner: processWarnings threshold failed", "threshold", threshold, "err", err)
		}
	}

	return nil
}
