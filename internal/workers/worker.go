package workers

import (
	"context"
	"control-plane/internal/model"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"time"
)

const (
	batchSize          = 5
	maxProcessAttempts = 10
)

const (
	msgInvoicePaid      = "Счёт оплачен. Готовим ваше подключение, ожидайте..."
	msgDeliveryFailed   = "Не удалось автоматически выдать доступ. Обязательно свяжитесь с саппортом — мы оперативно поможем. Кнопка «Поддержка» в главном меню."
	msgExpiringTemplate = "Подписка закончится через %s. Продлите сейчас через /start — без переподключения, потери трафика и новой ссылки."
	msgExpired          = "Подписка завершена. Нажмите /start, чтобы вернуть быстрый и безопасный интернет."
	msgRenewedTemplate  = "Подписка продлена до %s. Переподключаться не нужно."
)

func formatRemaining(d time.Duration) string {
	if d < time.Hour {
		return "час"
	}
	return fmt.Sprintf("~%d ч", int(math.Round(d.Hours())))
}

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

	case model.EventTypeSubscriptionRenewedNotification:
		var notify model.SubscriptionRenewedNotificationEvent
		if err := json.Unmarshal(event.Payload, &notify); err != nil {
			return err
		}
		return w.sender.Send(ctx, notify.ChatID, fmt.Sprintf(msgRenewedTemplate, notify.NewEndAt.Format("02.01.2006")))

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

	case model.EventTypeSubscriptionExpiringNotification:
		var ev model.SubscriptionExpiringNotificationEvent
		if err := json.Unmarshal(event.Payload, &ev); err != nil {
			return err
		}
		return w.sender.Send(ctx, ev.ChatID, fmt.Sprintf(msgExpiringTemplate, formatRemaining(ev.RemainingDuration)))

	case model.EventTypeSubscriptionExpiredNotification:
		var ev model.SubscriptionExpiredNotificationEvent
		if err := json.Unmarshal(event.Payload, &ev); err != nil {
			return err
		}
		return w.sender.Send(ctx, ev.ChatID, msgExpired)

	default:
		return fmt.Errorf("unknown event type: %s", event.EventType)
	}
}

// maybeEscalate fires a one-shot DeliveryFailedNotification when an event has
// exhausted its retry budget. Best-effort — we log and move on if it fails;
// the original event will still be marked failed by the caller.
func (w *Worker) maybeEscalate(ctx context.Context, event model.OutboxEvent, cause error) {
	if event.Attempts+1 < maxProcessAttempts {
		return
	}

	switch event.EventType {
	case model.EventTypeCredsDelivery:
		var delivery model.CredsDeliveryEvent
		if err := json.Unmarshal(event.Payload, &delivery); err != nil {
			slog.Error("escalate: unmarshal creds_delivery payload", "err", err)
			return
		}
		w.emitDeliveryFailed(ctx, delivery.SubscriptionID, delivery.ChatID,
			"creds_delivery exhausted — escalating to delivery_failed_notification", cause)

	case model.EventTypeSubscriptionActivated:
		var activated model.SubscriptionActivatedEvent
		if err := json.Unmarshal(event.Payload, &activated); err != nil {
			slog.Error("escalate: unmarshal subscription_activated payload", "err", err)
			return
		}
		if activated.ChatID == 0 {
			// Legacy in-flight event без chat_id — TG-уведомление не отправим.
			slog.Warn("escalate: subscription_activated has empty chat_id, skipping notification",
				"subscription_id", activated.SubscriptionID)
			return
		}
		w.emitDeliveryFailed(ctx, activated.SubscriptionID, activated.ChatID,
			"subscription_activated exhausted — escalating to delivery_failed_notification", cause)
	}
}

func (w *Worker) emitDeliveryFailed(ctx context.Context, subscriptionID string, chatID int64, msg string, cause error) {
	slog.Warn(msg, "subscription_id", subscriptionID, "chat_id", chatID, "cause", cause)

	failed := model.DeliveryFailedNotificationEvent{
		SubscriptionID: subscriptionID,
		ChatID:         chatID,
	}
	if err := w.storage.SaveOutboxEvent(ctx, model.EventTypeDeliveryFailedNotification, failed); err != nil {
		slog.Error("escalate: save delivery_failed_notification", "err", err)
	}
}
