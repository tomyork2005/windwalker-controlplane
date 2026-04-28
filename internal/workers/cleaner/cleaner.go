package cleaner

import (
	"context"
	"control-plane/internal/model"
	"log/slog"
	"time"
)

const (
	cleanerTickPeriod = 5 * time.Minute
	cleanerBatchSize  = 10
)

type CleanerStorage interface {
	ListExpiredActiveSubs(ctx context.Context, limit int) ([]*model.SubscriptionWithPlan, error)
	MarkSubscriptionInactive(ctx context.Context, id string) error
	SaveOutboxEvent(ctx context.Context, eventType string, payload any) error
	WithTx(ctx context.Context, fn func(ctx context.Context) error) error
}

type Cleaner struct {
	storage CleanerStorage
}

func NewCleaner(storage CleanerStorage) *Cleaner {
	return &Cleaner{storage: storage}
}

func (c *Cleaner) Run(ctx context.Context) {
	ticker := time.NewTicker(cleanerTickPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.processBatch(ctx); err != nil {
				slog.Error("cleaner: processBatch failed", "err", err)
			}
		}
	}
}

func (c *Cleaner) processBatch(ctx context.Context) error {
	return c.storage.WithTx(ctx, func(ctx context.Context) error {
		subs, err := c.storage.ListExpiredActiveSubs(ctx, cleanerBatchSize)
		if err != nil {
			return err
		}

		for _, sub := range subs {
			if err = c.storage.MarkSubscriptionInactive(ctx, sub.ID); err != nil {
				return err
			}

			if sub.AgentID == nil || *sub.AgentID == "" {
				continue
			}

			event := model.SubscriptionCancelEvent{
				UserID:         sub.UserID,
				AgentID:        *sub.AgentID,
				DriverType:     sub.DriverType,
				SubscriptionID: sub.ID,
			}
			if err = c.storage.SaveOutboxEvent(ctx, model.EventTypeSubscriptionCancelled, event); err != nil {
				return err
			}
		}

		return nil
	})
}
