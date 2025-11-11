package core

import (
	"context"
	"control-plane/internal/domain"
)

func (s *Service) HandleSubscribeActivationRequest(ctx context.Context, event domain.SubscriptionActivatedEvent) error {
	// 1. add to agent, get link
	// 2. send to bot
	return nil
}
