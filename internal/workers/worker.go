package workers

import (
	"context"
	"control-plane/internal/model"
	"encoding/json"
	"fmt"
	"time"
)

const (
	batchSize          = 5
	maxProcessAttempts = 10
)

type Storage interface {
	FetchUnprocessedOutboxEvents(ctx context.Context, limit int, attemptsLimit int) ([]model.OutboxEvent, error)
	MarkOutboxEventProcessed(ctx context.Context, id string) error
	MarkOutboxEventFailed(ctx context.Context, id string, errMsg string) error
}

type AgentSubscribeService interface {
	StartUserSubscribe(ctx context.Context, input model.SubscriptionActivatedEvent) error
	StopUserSubscribe(ctx context.Context, input model.SubscriptionCancelEvent) error
}

type Worker struct {
	storage Storage
	service AgentSubscribeService
}

func NewWorker(storage Storage, service AgentSubscribeService) *Worker {
	return &Worker{
		storage: storage,
		service: service,
	}
}

func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = w.processBatch(ctx)
		}
	}
}

func (w *Worker) processBatch(ctx context.Context) error {
	events, err := w.storage.FetchUnprocessedOutboxEvents(ctx, batchSize, maxProcessAttempts)
	if err != nil {
		return err
	}

	for _, event := range events {
		if err = w.resolveEvent(ctx, event); err != nil {
			if err = w.storage.MarkOutboxEventFailed(ctx, event.ID, err.Error()); err != nil {
				return err
			}
			continue
		}

		if err = w.storage.MarkOutboxEventProcessed(ctx, event.ID); err != nil {
			return err
		}
	}

	return nil
}

func (w *Worker) resolveEvent(ctx context.Context, event model.OutboxEvent) error {
	switch event.EventType {

	case model.EventTypeSubscriptionActivated:
		var active model.SubscriptionActivatedEvent
		if err := json.Unmarshal(event.Payload, &active); err != nil {
			return err
		}
		return w.service.StartUserSubscribe(ctx, active)

	case model.EventTypeSubscriptionCancelled:
		var cancel model.SubscriptionCancelEvent
		if err := json.Unmarshal(event.Payload, &cancel); err != nil {
			return err
		}
		return w.service.StopUserSubscribe(ctx, cancel)

	default:
		return fmt.Errorf("unknown event type: %s", event.EventType)
	}
}
