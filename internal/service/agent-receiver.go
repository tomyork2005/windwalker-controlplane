package service

import (
	"context"
	"control-plane/internal/model"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

type AgentReceiverStorage interface {
	UpsertAgent(ctx context.Context, agent *model.Agent) error
	UpdateAgentHeartbeat(ctx context.Context, agentID string, uptime uint64, seenAt, deadline time.Time) error
	ResolveChatIDBySubscribeID(ctx context.Context, subscribeID string) (int64, error)
	StoreSubscriptionCreds(ctx context.Context, subscriptionID string, creds string) error
	SaveOutboxEvent(ctx context.Context, eventType string, payload any) error

	WithTx(ctx context.Context, fn func(ctx context.Context) error) error
}

type VPNCreds interface {
	GetUserID() string
	ToTelegramClientOutput() string
}

type AgentReceiver struct {
	store AgentReceiverStorage

	hbTTL time.Duration
}

func NewAgentReceiver(store AgentReceiverStorage, hbTTL time.Duration) *AgentReceiver {
	if hbTTL <= 0 {
		hbTTL = 60 * time.Second
	}

	return &AgentReceiver{
		store: store,
		hbTTL: hbTTL,
	}
}

func (s *AgentReceiver) RegisterAgent(ctx context.Context, agent *model.Agent) error {
	if err := agent.Validate(); err != nil {
		return err
	}

	err := s.store.UpsertAgent(ctx, agent)
	if err != nil {
		slog.Error("Failed to register agent", "error", err)
		return fmt.Errorf("fail upsert agent: %w", err)
	}

	now := time.Now()
	err = s.store.UpdateAgentHeartbeat(ctx, agent.ID, 0, now, now.Add(s.hbTTL))
	if err != nil {
		slog.Error("Failed to update agent heartbeat", "error", err)
		return fmt.Errorf("fail update heartbeat: %w", err)
	}

	return nil
}

func (s *AgentReceiver) Heartbeat(ctx context.Context, agentID string, uptimeSeconds uint32) error {
	now := time.Now()
	return s.store.UpdateAgentHeartbeat(ctx, agentID, uint64(uptimeSeconds), now, now.Add(s.hbTTL))
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

	message := creds.ToTelegramClientOutput()

	return s.store.WithTx(ctx, func(ctx context.Context) error {
		if err := s.store.StoreSubscriptionCreds(ctx, subscribeID, message); err != nil {
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

func (s *AgentReceiver) HandleRemoveCallback(_ context.Context) error {
	return errors.New("not implemented yet")
}
func (s *AgentReceiver) HandleStatsAll(_ context.Context) error {
	return errors.New("not implemented yet")
}
func (s *AgentReceiver) HandleError(_ context.Context) error {
	return errors.New("not implemented yet")
}
