package outboxworker

import (
	"context"
	"control-plane/internal/domain"
	"encoding/json"
	"time"
)

type Record struct {
	ID      string
	Type    string
	Payload []byte
}

type storage interface {
	FetchUnprocessed(ctx context.Context, limit int) ([]Record, error)
	MarkProcessed(ctx context.Context, id string) error
}

type service interface {
	HandleSubscribeActivationRequest(ctx context.Context, event domain.SubscriptionActivatedEvent) error
}

type Worker struct {
	storage storage
	service service
}

func NewWorker(storage storage, service service) *Worker {
	return &Worker{
		storage: storage,
		service: service,
	}
}

func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	select {
	case <-ctx.Done():
		return
	case <-ticker.C:
		_ = w.processBatch(ctx)
	}
}

func (w *Worker) processBatch(ctx context.Context) error {
	records, err := w.storage.FetchUnprocessed(ctx, 5)
	if err != nil {
		return err
	}

	for _, record := range records {
		if err := w.handleRecord(ctx, record); err != nil {
			// todo logging
			continue
		}
		_ = w.storage.MarkProcessed(ctx, record.ID)
	}

	return nil
}

func (w *Worker) handleRecord(ctx context.Context, record Record) error {
	switch record.Type {

	case domain.EventTypeSubscriptionActivated:
		var event domain.SubscriptionActivatedEvent
		if err := json.Unmarshal(record.Payload, &event); err != nil {
			return err
		}
		return w.service.HandleSubscribeActivationRequest(ctx, event)

	default:
		// todo logging
		return nil
	}
}
