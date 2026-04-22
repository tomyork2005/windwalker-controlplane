package workers

import (
	"context"
	"control-plane/internal/model"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

const (
	batchSize          = 5
	maxProcessAttempts = 10
)

const (
	msgInvoicePaid    = "Счёт оплачен. Готовим ваше подключение, ожидайте..."
	msgDeliveryFailed = "Не удалось автоматически выдать доступ. Напишите в поддержку — мы уже разбираемся."
)

type Storage interface {
	FetchUnprocessedOutboxEvents(ctx context.Context, limit int, attemptsLimit int) ([]model.OutboxEvent, error)
	MarkOutboxEventProcessed(ctx context.Context, id string) error
	MarkOutboxEventFailed(ctx context.Context, id string, errMsg string) error
	SaveOutboxEvent(ctx context.Context, eventType string, payload any) error
}

type AgentSubscribeService interface {
	StartUserSubscribe(ctx context.Context, input model.SubscriptionActivatedEvent) error
	StopUserSubscribe(ctx context.Context, input model.SubscriptionCancelEvent) error
}

type TelegramSender interface {
	Send(ctx context.Context, chatID int64, message string) error
}

type Worker struct {
	storage Storage
	service AgentSubscribeService
	sender  TelegramSender
}

func NewWorker(storage Storage, service AgentSubscribeService, sender TelegramSender) *Worker {
	return &Worker{
		storage: storage,
		service: service,
		sender:  sender,
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
			w.maybeEscalate(ctx, event, err)
			if markErr := w.storage.MarkOutboxEventFailed(ctx, event.ID, err.Error()); markErr != nil {
				return markErr
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

	case model.EventTypeInvoicePaidNotification:
		var paid model.InvoicePaidNotificationEvent
		if err := json.Unmarshal(event.Payload, &paid); err != nil {
			return err
		}
		return w.sender.Send(ctx, paid.ChatID, msgInvoicePaid)

	case model.EventTypeCredsDelivery:
		var delivery model.CredsDeliveryEvent
		if err := json.Unmarshal(event.Payload, &delivery); err != nil {
			return err
		}
		return w.sender.Send(ctx, delivery.ChatID, delivery.Message)

	case model.EventTypeDeliveryFailedNotification:
		var failed model.DeliveryFailedNotificationEvent
		if err := json.Unmarshal(event.Payload, &failed); err != nil {
			return err
		}
		return w.sender.Send(ctx, failed.ChatID, msgDeliveryFailed)

	default:
		return fmt.Errorf("unknown event type: %s", event.EventType)
	}
}

// maybeEscalate fires a one-shot DeliveryFailedNotification when a creds_delivery
// event has exhausted its retry budget. Best-effort — we log and move on if it fails;
// the original event will still be marked failed by the caller.
func (w *Worker) maybeEscalate(ctx context.Context, event model.OutboxEvent, cause error) {
	if event.EventType != model.EventTypeCredsDelivery {
		return
	}
	if event.Attempts+1 < maxProcessAttempts {
		return
	}

	var delivery model.CredsDeliveryEvent
	if err := json.Unmarshal(event.Payload, &delivery); err != nil {
		slog.Error("escalate: unmarshal creds_delivery payload", "err", err)
		return
	}

	slog.Warn("creds_delivery exhausted — escalating to delivery_failed_notification",
		"subscription_id", delivery.SubscriptionID, "chat_id", delivery.ChatID, "cause", cause)

	failed := model.DeliveryFailedNotificationEvent{
		SubscriptionID: delivery.SubscriptionID,
		ChatID:         delivery.ChatID,
	}
	if err := w.storage.SaveOutboxEvent(ctx, model.EventTypeDeliveryFailedNotification, failed); err != nil {
		slog.Error("escalate: save delivery_failed_notification", "err", err)
	}
}
