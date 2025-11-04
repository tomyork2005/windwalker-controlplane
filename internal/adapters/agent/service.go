package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

const (
	taskUpsertConnection = "upsert"
	taskRemoveConnection = "remove"
)

type storage interface {
	UpsertAgent(ctx context.Context, a Agent) error
	UpdateAgentHeartbeat(ctx context.Context, agentID string, uptime uint64, seenAt, deadline time.Time) error
	MarkTaskAck(ctx context.Context, agentID string, seq uint64) error
	MarkTaskNack(ctx context.Context, agentID string, seq uint64, errMsg string) error
	MarkTaskSent(ctx context.Context, agentID string, seq uint64) error

	ChooseBestAgent(ctx context.Context, driverType string, region string) (string, error)
	FindAgentIDByUserID(ctx context.Context, userID string) (string, error)
	NextSeq(ctx context.Context, agentID string) (uint64, error)
	EnqueueTask(ctx context.Context, task OutboxTask) error
}

type TxManager interface {
	WithTx(context.Context, func(ctx context.Context) error) error
}

type Clock interface {
	Now() time.Time
}

type Service struct {
	store storage
	clock Clock
	tx    TxManager
	reg   *registry

	hbTTL time.Duration
}

func NewService(store storage, reg *registry, clock Clock, hbTTL time.Duration) *Service {
	if hbTTL <= 0 {
		hbTTL = 60 * time.Second
	}

	return &Service{
		store: store,
		reg:   reg,
		clock: clock,
		hbTTL: hbTTL,
	}
}

// From agents

func (s *Service) UpsertAgent(ctx context.Context, agent Agent) error {
	if err := agent.Validate(); err != nil {
		return err
	}

	err := s.store.UpsertAgent(ctx, agent)
	if err != nil {
		return fmt.Errorf("fail update heartbeat: %w", err)
	}

	now := s.clock.Now()
	err = s.store.UpdateAgentHeartbeat(ctx, agent.ID, 0, now, now.Add(s.hbTTL))
	if err != nil {
		return fmt.Errorf("fail update heartbeat: %w", err)
	}

	return nil
}

func (s *Service) Heartbeat(ctx context.Context, agentID string, uptimeSeconds uint32) error {
	now := s.clock.Now()
	return s.store.UpdateAgentHeartbeat(ctx, agentID, uint64(uptimeSeconds), now, now.Add(s.hbTTL))
}

func (s *Service) Ack(ctx context.Context, agentID string, seq uint64) error {
	return s.store.MarkTaskAck(ctx, agentID, seq)
}

func (s *Service) Nack(ctx context.Context, agentID string, seq uint64, errMsg string) error {
	return s.store.MarkTaskNack(ctx, agentID, seq, errMsg)
}

// From other control-plane components

func (s *Service) UpsertUser(ctx context.Context, input UserUpsertInput) error {
	err := input.Validate()
	if err != nil {
		return fmt.Errorf("fail upsert user validation: %w", err)
	}

	agentID, err := s.store.ChooseBestAgent(ctx, input.DriverType, input.Region)
	if err != nil {
		return fmt.Errorf("fail upsert user storage: %w", err)
	}

	expiresAt := input.SubscribeTime.UntilOr(s.clock.Now())
	up := &UpsertPayload{UserID: input.UserID, DriverType: input.DriverType, ExpiresAt: expiresAt}
	payload, _ := json.Marshal(up)

	var seq uint64
	err = s.tx.WithTx(ctx, func(ctx context.Context) error {
		seq, err = s.store.NextSeq(ctx, agentID)
		if err != nil {
			return err
		}

		err = s.store.EnqueueTask(ctx, OutboxTask{
			AgentID:   agentID,
			Seq:       seq,
			RequestID: input.RequestID,
			Kind:      OpUpsert,
			Payload:   payload,
		})
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("fail upsert user storage: %w", err)
	}

	s.trySendNow(Operation{
		AgentID:   agentID,
		Seq:       seq,
		RequestID: input.RequestID,
		Kind:      OpUpsert,
		Upsert:    up,
	})

	return nil
}

func (s *Service) RemoveUser(ctx context.Context, input RemoveUserInput) error {
	err := input.Validate()
	if err != nil {
		return fmt.Errorf("fail remove user validation: %w", err)
	}

	agentID, err := s.store.FindAgentIDByUserID(ctx, input.UserID)
	if err != nil {
		return fmt.Errorf("fail remove user storage: %w", err)
	}

	rm := &RemovePayload{UserID: input.UserID, DriverType: input.DriverType}
	payload, _ := json.Marshal(rm)

	var seq uint64
	err = s.tx.WithTx(ctx, func(ctx context.Context) error {
		seq, err = s.store.NextSeq(ctx, agentID)
		if err != nil {
			return err
		}

		err = s.store.EnqueueTask(ctx, OutboxTask{
			AgentID:   agentID,
			Seq:       seq,
			RequestID: input.RequestID,
			Kind:      OpRemove,
			Payload:   payload,
		})
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("fail remove user storage: %w", err)
	}

	s.trySendNow(Operation{
		AgentID:   agentID,
		RequestID: input.RequestID,
		Seq:       seq,
		Kind:      OpRemove,
		Remove:    rm,
	})

	return nil
}

func (s *Service) trySendNow(op Operation) {
	if s.reg == nil {
		return
	}

	if s.reg.trySend(op) {
		_ = s.store.MarkTaskSent(context.Background(), op.AgentID, op.Seq)
	}
}
