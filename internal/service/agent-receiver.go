package service

import (
	"context"
	"control-plane/internal/model"
	"fmt"
	"html"
	"log/slog"
	"time"
)

type AgentReceiverStorage interface {
	UpsertAgent(ctx context.Context, agent *model.Agent) error
	UpdateAgentStats(ctx context.Context, agentID string, uptime uint64, seenAt, deadline time.Time) error
	UpsertUserTrafficBatch(ctx context.Context, agentID string, windowEnd time.Time, rows []model.UserUsageRow) error
	ResolveChatIDBySubscribeID(ctx context.Context, subscribeID string) (int64, error)
	StoreSubscriptionCreds(ctx context.Context, subscriptionID string, creds string) error
	SaveOutboxEvent(ctx context.Context, eventType string, payload any) error

	WithTx(ctx context.Context, fn func(ctx context.Context) error) error
}

type VPNCreds interface {
	GetUserID() string
	GetLink() string
}

type AgentReceiver struct {
	store    AgentReceiverStorage
	statsTTL time.Duration
}

func NewAgentReceiver(store AgentReceiverStorage, statsTTL time.Duration) *AgentReceiver {
	if statsTTL <= 0 {
		statsTTL = 90 * time.Second
	}

	return &AgentReceiver{
		store:    store,
		statsTTL: statsTTL,
	}
}

func (s *AgentReceiver) RegisterAgent(ctx context.Context, agent *model.Agent) error {
	if err := agent.Validate(); err != nil {
		return err
	}

	if err := s.store.UpsertAgent(ctx, agent); err != nil {
		slog.Error("Failed to register agent", "error", err)
		return fmt.Errorf("fail upsert agent: %w", err)
	}

	return nil
}

func (s *AgentReceiver) HandleStats(ctx context.Context, stats model.AgentStats) error {
	if stats.AgentID == "" {
		return fmt.Errorf("handle stats: empty agent_id")
	}

	now := time.Now()
	return s.store.WithTx(ctx, func(ctx context.Context) error {
		if err := s.store.UpdateAgentStats(ctx, stats.AgentID, uint64(stats.UptimeSeconds), now, now.Add(s.statsTTL)); err != nil {
			return fmt.Errorf("update agent stats: %w", err)
		}
		if len(stats.Users) == 0 {
			return nil
		}
		if err := s.store.UpsertUserTrafficBatch(ctx, stats.AgentID, stats.WindowEnd, stats.Users); err != nil {
			return fmt.Errorf("upsert user traffic batch: %w", err)
		}
		return nil
	})
}

func (s *AgentReceiver) HandleStartUserSubscribeResponse(ctx context.Context, subscribeID string, creds VPNCreds) error {
	if subscribeID == "" {
		slog.Error("Agent upsert response has empty request_id — orphan creds, cannot deliver",
			"user_id", creds.GetUserID())
		return nil
	}

	chatID, err := s.store.ResolveChatIDBySubscribeID(ctx, subscribeID)
	if err != nil {
		return fmt.Errorf("fail resolve chat id: %w", err)
	}

	link := creds.GetLink()
	message := fmt.Sprintf("🕊️ Подключение готово!\n\n<code>%s</code>\n\nИнструкция — в «Моя подписка».", html.EscapeString(link))

	return s.store.WithTx(ctx, func(ctx context.Context) error {
		if err := s.store.StoreSubscriptionCreds(ctx, subscribeID, link); err != nil {
			return fmt.Errorf("fail store subscription creds: %w", err)
		}

		event := model.CredsDeliveryEvent{
			SubscriptionID: subscribeID,
			ChatID:         chatID,
			Message:        message,
		}
		if err := s.store.SaveOutboxEvent(ctx, model.EventTypeCredsDelivery, event); err != nil {
			return fmt.Errorf("fail save creds delivery event: %w", err)
		}
		return nil
	})
}
