package service

import (
	"context"
	"control-plane/internal/model"
	"fmt"
	"time"
)

type AgentSenderStorage interface {
	ChooseBestAgent(ctx context.Context, driverType string, region string) (string, error)
	FindAgentIDByUserID(ctx context.Context, userID string) (string, error)
}

type AgentDispatcher interface {
	DispatchUpsert(ctx context.Context, agentID string, subscriptionID string, payload *model.AgentUpsertPayload) error
	DispatchRemove(ctx context.Context, agentID string, subscriptionID string, payload *model.AgentRemovePayload) error
}

type AgentSender struct {
	store      AgentSenderStorage
	dispatcher AgentDispatcher
}

func NewAgentService(store AgentSenderStorage, dispatcher AgentDispatcher) *AgentSender {
	return &AgentSender{
		store:      store,
		dispatcher: dispatcher,
	}
}

func (s *AgentSender) StartUserSubscribe(ctx context.Context, input model.SubscriptionActivatedEvent) error {
	// agent with less numb of users
	agentID, err := s.store.ChooseBestAgent(ctx, input.DriverType, input.Region)
	if err != nil {
		return fmt.Errorf("fail upsert user storage: %w", err)
	}

	expiresAt := time.Now().Add(input.SubscribeDuration)
	payload := &model.AgentUpsertPayload{
		UserID:     input.UserID,
		DriverType: input.DriverType,
		ExpiresAt:  expiresAt,
	}

	if err = s.dispatcher.DispatchUpsert(ctx, agentID, input.SubscriptionID, payload); err != nil {
		return fmt.Errorf("dispatch upsert user: %w", err)
	}

	return nil
}

func (s *AgentSender) RemoveUser(ctx context.Context, input model.RemoveUserInput) error {
	agentID, err := s.store.FindAgentIDByUserID(ctx, input.UserID)
	if err != nil {
		return fmt.Errorf("fail remove user storage: %w", err)
	}

	rm := &model.AgentRemovePayload{
		UserID:     input.UserID,
		DriverType: input.DriverType,
	}

	if err = s.dispatcher.DispatchRemove(ctx, agentID, input.RequestID, rm); err != nil {
		return fmt.Errorf("dispatch remove user: %w", err)
	}

	return nil
}
