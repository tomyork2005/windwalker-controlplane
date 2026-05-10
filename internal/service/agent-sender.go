package service

import (
	"context"
	"control-plane/internal/model"
	"errors"
	"fmt"
	"log/slog"
)

type AgentSenderStorage interface {
	ChooseBestAgent(ctx context.Context, driverType string, region string) (string, error)
	BindSubscriptionToAgent(ctx context.Context, subscriptionID string, agentID string) error

	WithTx(ctx context.Context, fn func(ctx context.Context) error) error
}

type AgentDispatcher interface {
	DispatchUpsert(ctx context.Context, agentID string, subscriptionID string, payload *model.AgentUpsertPayload) (*model.Operation, error)
	DispatchRemove(ctx context.Context, agentID string, subscriptionID string, payload *model.AgentRemovePayload) error

	TryDispatchPrepared(ctx context.Context, op *model.Operation)
}

type AgentSender struct {
	store      AgentSenderStorage
	dispatcher AgentDispatcher
}

func NewAgentSender(store AgentSenderStorage, dispatcher AgentDispatcher) *AgentSender {
	return &AgentSender{
		store:      store,
		dispatcher: dispatcher,
	}
}

func (s *AgentSender) StartUserSubscribe(ctx context.Context, input model.SubscriptionActivatedEvent) error {
	payload := &model.AgentUpsertPayload{
		UserID:     input.UserID,
		DriverType: input.DriverType,
	}

	var op *model.Operation
	err := s.store.WithTx(ctx, func(ctx context.Context) error {
		agentID, err := s.store.ChooseBestAgent(ctx, input.DriverType, input.Region) // less numb of users
		if err != nil {
			return fmt.Errorf("fail upsert user storage: %w", err)
		}

		err = s.store.BindSubscriptionToAgent(ctx, input.SubscriptionID, agentID)
		if err != nil {
			return fmt.Errorf("fail upsert subscription storage: %w", err)
		}

		op, err = s.dispatcher.DispatchUpsert(ctx, agentID, input.SubscriptionID, payload)
		return err
	})
	if err != nil {
		slog.Error("Failed to start user subscribe", "err", err)
		return fmt.Errorf("fail start user subscribe: %w", err)
	}

	if op == nil {
		slog.Error("Failed to start user subscribe: operation is nil")
		return errors.New("operation is nil")
	}

	s.dispatcher.TryDispatchPrepared(ctx, op)
	return nil
}

func (s *AgentSender) StopUserSubscribe(ctx context.Context, input model.SubscriptionCancelEvent) error {
	if input.AgentID == "" {
		return fmt.Errorf("stop user subscribe: empty agent_id in event for subscription %s", input.SubscriptionID)
	}

	rm := &model.AgentRemovePayload{
		UserID:     input.UserID,
		DriverType: input.DriverType,
	}

	if err := s.dispatcher.DispatchRemove(ctx, input.AgentID, input.SubscriptionID, rm); err != nil {
		return fmt.Errorf("dispatch remove user: %w", err)
	}

	return nil
}
